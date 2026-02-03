package handlers

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"dns-dashboard/db"
	"dns-dashboard/models"

	"github.com/gofiber/fiber/v2"
)

func ApiStats(c *fiber.Ctx) error {
	stats := models.DashboardStats{}

	// Optimized: Single query instead of 5 separate queries
	err := db.DB.QueryRow(`
		SELECT 
			countIf(response_type = 'CQ') as total_queries,
			countIf(response_type = 'CQ' AND timestamp >= today()) as today_queries,
			uniqIf(client_ip, response_type = 'CQ' AND timestamp >= today()) as unique_clients,
			uniqIf(qname, response_type = 'CQ' AND timestamp >= today()) as unique_domains,
			countIf(response_type = 'CQ' AND timestamp >= now() - INTERVAL 1 MINUTE) / 60.0 as qps
		FROM dns_logs
	`).Scan(&stats.TotalQueries, &stats.TodayQueries, &stats.UniqueClients, &stats.UniqueDomains, &stats.QPS)
	if err != nil {
		log.Printf("ApiStats query failed: %v", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "database error"})
	}

	// Fetch cache hit ratio from dnsdist API
	stats.CacheHitRatio = fetchDnsdistCacheHitRatio()

	return c.JSON(stats)
}

// fetchDnsdistCacheHitRatio fetches cache statistics from dnsdist web API
func fetchDnsdistCacheHitRatio() float64 {
	dnsdistURL := os.Getenv("DNSDIST_API_URL")
	if dnsdistURL == "" {
		dnsdistURL = "http://127.0.0.1:8083"
	}
	apiKey := os.Getenv("DNSDIST_API_KEY")
	if apiKey == "" {
		apiKey = "supersecretAPIkey"
	}

	client := &http.Client{Timeout: 2 * time.Second}

	// Try the jsonstat endpoint first (simpler format)
	req, err := http.NewRequest("GET", dnsdistURL+"/jsonstat?command=stats", nil)
	if err != nil {
		log.Printf("dnsdist API request error: %v", err)
		return -1
	}
	req.Header.Set("X-API-Key", apiKey)

	resp, err := client.Do(req)
	if err != nil {
		log.Printf("dnsdist API connection error: %v", err)
		return -1
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		log.Printf("dnsdist API status: %d", resp.StatusCode)
		return -1
	}

	// Parse JSON response - dnsdist returns object with stats
	var statsMap map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&statsMap); err != nil {
		log.Printf("dnsdist API JSON decode error: %v", err)
		return -1
	}

	// Extract cache-hits and cache-misses
	var cacheHits, cacheMisses float64
	if v, ok := statsMap["cache-hits"]; ok {
		cacheHits, _ = v.(float64)
	}
	if v, ok := statsMap["cache-misses"]; ok {
		cacheMisses, _ = v.(float64)
	}

	total := cacheHits + cacheMisses
	if total == 0 {
		return 0
	}
	return cacheHits / total * 100
}

// ApiDnsdistStats returns all dnsdist statistics
func ApiDnsdistStats(c *fiber.Ctx) error {
	dnsdistURL := os.Getenv("DNSDIST_API_URL")
	if dnsdistURL == "" {
		dnsdistURL = "http://127.0.0.1:8083"
	}
	apiKey := os.Getenv("DNSDIST_API_KEY")
	if apiKey == "" {
		apiKey = "supersecretAPIkey"
	}

	client := &http.Client{Timeout: 2 * time.Second}
	req, err := http.NewRequest("GET", dnsdistURL+"/jsonstat?command=stats", nil)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "request error"})
	}
	req.Header.Set("X-API-Key", apiKey)

	resp, err := client.Do(req)
	if err != nil {
		return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{"error": "dnsdist unavailable"})
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return c.Status(fiber.StatusBadGateway).JSON(fiber.Map{"error": "dnsdist error"})
	}

	var statsMap map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&statsMap); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "parse error"})
	}

	// Extract useful metrics
	getFloat := func(key string) float64 {
		if v, ok := statsMap[key]; ok {
			if f, ok := v.(float64); ok {
				return f
			}
		}
		return 0
	}

	result := fiber.Map{
		// Cache
		"cache_hits":      getFloat("cache-hits"),
		"cache_misses":    getFloat("cache-misses"),
		"cache_hit_ratio": 0.0,

		// Queries
		"queries":   getFloat("queries"),
		"responses": getFloat("responses"),

		// Response codes
		"frontend_noerror":  getFloat("frontend-noerror"),
		"frontend_nxdomain": getFloat("frontend-nxdomain"),
		"frontend_servfail": getFloat("frontend-servfail"),

		// Latency (microseconds)
		"latency_avg100":   getFloat("latency-avg100") / 1000,   // to ms
		"latency_avg1000":  getFloat("latency-avg1000") / 1000,  // to ms
		"latency_avg10000": getFloat("latency-avg10000") / 1000, // to ms

		// Latency histogram
		"latency_0_1":      getFloat("latency0-1"),
		"latency_1_10":     getFloat("latency1-10"),
		"latency_10_50":    getFloat("latency10-50"),
		"latency_50_100":   getFloat("latency50-100"),
		"latency_100_1000": getFloat("latency100-1000"),
		"latency_slow":     getFloat("latency-slow"),

		// Errors & Security
		"downstream_timeouts": getFloat("downstream-timeouts"),
		"acl_drops":           getFloat("acl-drops"),
		"dyn_blocked":         getFloat("dyn-blocked"),
		"rule_drop":           getFloat("rule-drop"),
		"rule_refused":        getFloat("rule-refused"),

		// System
		"uptime":       getFloat("uptime"),
		"memory_usage": getFloat("real-memory-usage") / 1024 / 1024, // to MB
		"cpu_user_ms":  getFloat("cpu-user-msec"),
		"cpu_sys_ms":   getFloat("cpu-sys-msec"),
	}

	// Calculate cache hit ratio
	cacheHits := getFloat("cache-hits")
	cacheMisses := getFloat("cache-misses")
	if total := cacheHits + cacheMisses; total > 0 {
		result["cache_hit_ratio"] = cacheHits / total * 100
	}

	return c.JSON(result)
}

func ApiQueryTypes(c *fiber.Ctx) error {
	rows, err := db.DB.Query(`
		SELECT qtype, count() as cnt 
		FROM dns_logs 
		WHERE response_type = 'CQ' AND timestamp >= today() 
		GROUP BY qtype 
		ORDER BY cnt DESC 
		LIMIT 10
	`)
	if err != nil {
		log.Printf("ApiQueryTypes query failed: %v", err)
		return c.JSON([]models.QueryTypeStats{})
	}
	defer rows.Close()

	var results []models.QueryTypeStats
	for rows.Next() {
		var s models.QueryTypeStats
		var qtype uint16
		if err := rows.Scan(&qtype, &s.Count); err != nil {
			log.Printf("ApiQueryTypes scan failed: %v", err)
			continue
		}
		s.Type = qtypeToString(qtype)
		results = append(results, s)
	}
	if err := rows.Err(); err != nil {
		log.Printf("ApiQueryTypes rows error: %v", err)
	}
	return c.JSON(results)
}

func ApiResponseCodes(c *fiber.Ctx) error {
	rows, err := db.DB.Query(`
		SELECT rcode, count() as cnt 
		FROM dns_logs 
		WHERE response_type = 'CR' AND timestamp >= today() 
		GROUP BY rcode 
		ORDER BY cnt DESC
	`)
	if err != nil {
		log.Printf("ApiResponseCodes query failed: %v", err)
		return c.JSON([]models.ResponseCodeStats{})
	}
	defer rows.Close()

	var results []models.ResponseCodeStats
	for rows.Next() {
		var s models.ResponseCodeStats
		var rcode uint8
		if err := rows.Scan(&rcode, &s.Count); err != nil {
			log.Printf("ApiResponseCodes scan failed: %v", err)
			continue
		}
		s.Code = rcodeToString(rcode)
		results = append(results, s)
	}
	if err := rows.Err(); err != nil {
		log.Printf("ApiResponseCodes rows error: %v", err)
	}
	return c.JSON(results)
}

func ApiTopDomains(c *fiber.Ctx) error {
	rows, err := db.DB.Query(`
		SELECT qname, count() as cnt 
		FROM dns_logs 
		WHERE response_type = 'CQ' AND timestamp >= today() AND qname != ''
		GROUP BY qname 
		ORDER BY cnt DESC 
		LIMIT 20
	`)
	if err != nil {
		log.Printf("ApiTopDomains query failed: %v", err)
		return c.JSON([]models.TopDomain{})
	}
	defer rows.Close()

	var results []models.TopDomain
	for rows.Next() {
		var s models.TopDomain
		if err := rows.Scan(&s.Domain, &s.Count); err != nil {
			log.Printf("ApiTopDomains scan failed: %v", err)
			continue
		}
		results = append(results, s)
	}
	if err := rows.Err(); err != nil {
		log.Printf("ApiTopDomains rows error: %v", err)
	}
	return c.JSON(results)
}

func ApiTopClients(c *fiber.Ctx) error {
	rows, err := db.DB.Query(`
		SELECT replaceOne(toString(client_ip), '::ffff:', '') as client_ip, count() as cnt 
		FROM dns_logs 
		WHERE response_type = 'CQ' AND timestamp >= today()
		GROUP BY client_ip 
		ORDER BY cnt DESC 
		LIMIT 20
	`)
	if err != nil {
		log.Printf("ApiTopClients query failed: %v", err)
		return c.JSON([]models.TopClient{})
	}
	defer rows.Close()

	var results []models.TopClient
	for rows.Next() {
		var s models.TopClient
		if err := rows.Scan(&s.IP, &s.Count); err != nil {
			log.Printf("ApiTopClients scan failed: %v", err)
			continue
		}
		results = append(results, s)
	}
	if err := rows.Err(); err != nil {
		log.Printf("ApiTopClients rows error: %v", err)
	}
	return c.JSON(results)
}

func ApiRecentQueries(c *fiber.Ctx) error {
	rows, err := db.DB.Query(`
		SELECT 
			formatDateTime(timestamp, '%Y-%m-%d %H:%i:%S') as ts,
			replaceOne(toString(client_ip), '::ffff:', '') as client_ip, qname, qtype, response_type 
		FROM dns_logs 
		WHERE response_type = 'CQ' 
		ORDER BY timestamp DESC 
		LIMIT 50
	`)
	if err != nil {
		log.Printf("ApiRecentQueries query failed: %v", err)
		return c.JSON([]models.RecentQuery{})
	}
	defer rows.Close()

	var results []models.RecentQuery
	for rows.Next() {
		var q models.RecentQuery
		var qtype uint16
		if err := rows.Scan(&q.Timestamp, &q.ClientIP, &q.Domain, &qtype, &q.ResponseType); err != nil {
			log.Printf("ApiRecentQueries scan failed: %v", err)
			continue
		}
		q.Type = qtypeToString(qtype)
		results = append(results, q)
	}
	if err := rows.Err(); err != nil {
		log.Printf("ApiRecentQueries rows error: %v", err)
	}
	return c.JSON(results)
}

func ApiTimeline(c *fiber.Ctx) error {
	rows, err := db.DB.Query(`
		SELECT 
			toStartOfMinute(timestamp) as minute,
			count() as cnt
		FROM dns_logs 
		WHERE response_type = 'CQ' AND timestamp >= now() - INTERVAL 1 HOUR
		GROUP BY minute
		ORDER BY minute
	`)
	if err != nil {
		log.Printf("ApiTimeline query failed: %v", err)
		return c.JSON([]map[string]interface{}{})
	}
	defer rows.Close()

	var results []map[string]interface{}
	for rows.Next() {
		var minute time.Time
		var count int64
		if err := rows.Scan(&minute, &count); err != nil {
			log.Printf("ApiTimeline scan failed: %v", err)
			continue
		}
		results = append(results, map[string]interface{}{
			"time":  minute.Format("15:04"),
			"count": count,
		})
	}
	if err := rows.Err(); err != nil {
		log.Printf("ApiTimeline rows error: %v", err)
	}
	return c.JSON(results)
}

func ApiLogs(c *fiber.Ctx) error {
	page := c.QueryInt("page", 1)
	limit := c.QueryInt("limit", 50)
	if page < 1 {
		page = 1
	}
	if limit <= 0 {
		limit = 50
	}
	if limit > 500 {
		limit = 500
	}

	clientIP := strings.TrimSpace(c.Query("client_ip"))
	domain := strings.TrimSpace(c.Query("domain"))
	qtype := strings.TrimSpace(c.Query("type"))
	responseType := strings.ToUpper(strings.TrimSpace(c.Query("response_type")))
	from := strings.TrimSpace(c.Query("from"))
	to := strings.TrimSpace(c.Query("to"))
	order := strings.ToLower(strings.TrimSpace(c.Query("order", "desc")))
	if order != "asc" {
		order = "desc"
	}

	offset := (page - 1) * limit

	where := " WHERE 1=1"
	args := []interface{}{}

	if clientIP != "" {
		// Search in string representation which includes both formats
		where += " AND toString(client_ip) LIKE ?"
		args = append(args, "%"+clientIP+"%")
	}
	if domain != "" {
		where += " AND qname LIKE ?"
		args = append(args, "%"+domain+"%")
	}
	if qtype != "" {
		qt, ok := parseQType(qtype)
		if !ok {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid qtype"})
		}
		where += " AND qtype = ?"
		args = append(args, qt)
	}
	if responseType != "" {
		if responseType != "CQ" && responseType != "CR" {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid response_type"})
		}
		where += " AND response_type = ?"
		args = append(args, responseType)
	}
	if from != "" {
		where += " AND timestamp >= parseDateTimeBestEffort(?)"
		args = append(args, from)
	}
	if to != "" {
		where += " AND timestamp <= parseDateTimeBestEffort(?)"
		args = append(args, to)
	}

	query := `
		SELECT 
			formatDateTime(timestamp, '%Y-%m-%d %H:%i:%S') as ts,
			replaceOne(toString(client_ip), '::ffff:', '') as client_ip, qname, qtype, response_type, response_size
		FROM dns_logs
	` + where + fmt.Sprintf(" ORDER BY timestamp %s LIMIT %d OFFSET %d", order, limit, offset)

	rows, err := db.DB.Query(query, args...)
	if err != nil {
		log.Printf("ApiLogs query failed: %v", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "database error"})
	}
	defer rows.Close()

	var results []map[string]interface{}
	for rows.Next() {
		var ts, ip, qname, rtype string
		var qtype uint16
		var size int
		if err := rows.Scan(&ts, &ip, &qname, &qtype, &rtype, &size); err != nil {
			log.Printf("ApiLogs scan failed: %v", err)
			continue
		}
		results = append(results, map[string]interface{}{
			"timestamp":     ts,
			"client_ip":     ip,
			"domain":        qname,
			"type":          qtypeToString(qtype),
			"response_type": rtype,
			"size":          size,
		})
	}
	if err := rows.Err(); err != nil {
		log.Printf("ApiLogs rows error: %v", err)
	}

	var total int64
	countQuery := "SELECT count() FROM dns_logs" + where
	if err := db.DB.QueryRow(countQuery, args...).Scan(&total); err != nil {
		log.Printf("ApiLogs count query failed: %v", err)
	}

	return c.JSON(fiber.Map{
		"data":  results,
		"total": total,
		"page":  page,
		"limit": limit,
	})
}

func qtypeToString(qtype uint16) string {
	if name, ok := qtypeNameByValue[qtype]; ok {
		return name
	}
	return strconv.Itoa(int(qtype))
}

func rcodeToString(rcode uint8) string {
	if name, ok := rcodeNameByValue[rcode]; ok {
		return fmt.Sprintf("%s (%d)", name, rcode)
	}
	return strconv.Itoa(int(rcode))
}

func parseQType(input string) (uint16, bool) {
	s := strings.ToUpper(strings.TrimSpace(input))
	if s == "" {
		return 0, false
	}
	if n, err := strconv.Atoi(s); err == nil {
		return uint16(n), true
	}
	if v, ok := qtypeValueByName[s]; ok {
		return v, true
	}
	return 0, false
}

var qtypeNameByValue = map[uint16]string{
	1:   "A",
	2:   "NS",
	5:   "CNAME",
	6:   "SOA",
	12:  "PTR",
	15:  "MX",
	16:  "TXT",
	28:  "AAAA",
	33:  "SRV",
	64:  "SVCB",
	65:  "HTTPS",
	255: "ANY",
}

var qtypeValueByName = map[string]uint16{
	"A":     1,
	"NS":    2,
	"CNAME": 5,
	"SOA":   6,
	"PTR":   12,
	"MX":    15,
	"TXT":   16,
	"AAAA":  28,
	"SRV":   33,
	"SVCB":  64,
	"HTTPS": 65,
	"ANY":   255,
}

var rcodeNameByValue = map[uint8]string{
	0:  "NOERROR",
	1:  "FORMERR",
	2:  "SERVFAIL",
	3:  "NXDOMAIN",
	4:  "NOTIMP",
	5:  "REFUSED",
	6:  "YXDOMAIN",
	7:  "YXRRSET",
	8:  "NXRRSET",
	9:  "NOTAUTH",
	10: "NOTZONE",
}

package handlers

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"dns-dashboard/db"
	"dns-dashboard/models"

	"github.com/gofiber/fiber/v2"
)

func ApiStats(c *fiber.Ctx) error {
	stats := models.DashboardStats{}

	db.DB.QueryRow("SELECT count() FROM dns_logs WHERE response_type = 'CQ'").Scan(&stats.TotalQueries)
	db.DB.QueryRow("SELECT count() FROM dns_logs WHERE response_type = 'CQ' AND timestamp >= today()").Scan(&stats.TodayQueries)
	db.DB.QueryRow("SELECT uniq(client_ip) FROM dns_logs WHERE response_type = 'CQ' AND timestamp >= today()").Scan(&stats.UniqueClients)
	db.DB.QueryRow("SELECT uniq(qname) FROM dns_logs WHERE response_type = 'CQ' AND timestamp >= today()").Scan(&stats.UniqueDomains)
	db.DB.QueryRow("SELECT count() / 60.0 FROM dns_logs WHERE response_type = 'CQ' AND timestamp >= now() - INTERVAL 1 MINUTE").Scan(&stats.QPS)
	stats.CacheHitRatio = -1

	return c.JSON(stats)
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
		return c.JSON([]models.QueryTypeStats{})
	}
	defer rows.Close()

	var results []models.QueryTypeStats
	for rows.Next() {
		var s models.QueryTypeStats
		var qtype uint16
		rows.Scan(&qtype, &s.Count)
		s.Type = qtypeToString(qtype)
		results = append(results, s)
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
		return c.JSON([]models.ResponseCodeStats{})
	}
	defer rows.Close()

	var results []models.ResponseCodeStats
	for rows.Next() {
		var s models.ResponseCodeStats
		var rcode uint8
		rows.Scan(&rcode, &s.Count)
		s.Code = rcodeToString(rcode)
		results = append(results, s)
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
		return c.JSON([]models.TopDomain{})
	}
	defer rows.Close()

	var results []models.TopDomain
	for rows.Next() {
		var s models.TopDomain
		rows.Scan(&s.Domain, &s.Count)
		results = append(results, s)
	}
	return c.JSON(results)
}

func ApiTopClients(c *fiber.Ctx) error {
	rows, err := db.DB.Query(`
		SELECT client_ip, count() as cnt 
		FROM dns_logs 
		WHERE response_type = 'CQ' AND timestamp >= today()
		GROUP BY client_ip 
		ORDER BY cnt DESC 
		LIMIT 20
	`)
	if err != nil {
		return c.JSON([]models.TopClient{})
	}
	defer rows.Close()

	var results []models.TopClient
	for rows.Next() {
		var s models.TopClient
		rows.Scan(&s.IP, &s.Count)
		results = append(results, s)
	}
	return c.JSON(results)
}

func ApiRecentQueries(c *fiber.Ctx) error {
	rows, err := db.DB.Query(`
		SELECT 
			formatDateTime(timestamp, '%Y-%m-%d %H:%M:%S') as ts,
			client_ip, qname, qtype, response_type 
		FROM dns_logs 
		WHERE response_type = 'CQ' 
		ORDER BY timestamp DESC 
		LIMIT 50
	`)
	if err != nil {
		return c.JSON([]models.RecentQuery{})
	}
	defer rows.Close()

	var results []models.RecentQuery
	for rows.Next() {
		var q models.RecentQuery
		var qtype uint16
		rows.Scan(&q.Timestamp, &q.ClientIP, &q.Domain, &qtype, &q.ResponseType)
		q.Type = qtypeToString(qtype)
		results = append(results, q)
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
		return c.JSON([]map[string]interface{}{})
	}
	defer rows.Close()

	var results []map[string]interface{}
	for rows.Next() {
		var minute time.Time
		var count int64
		rows.Scan(&minute, &count)
		results = append(results, map[string]interface{}{
			"time":  minute.Format("15:04"),
			"count": count,
		})
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
		where += " AND client_ip = ?"
		args = append(args, clientIP)
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
			formatDateTime(timestamp, '%Y-%m-%d %H:%M:%S') as ts,
			client_ip, qname, qtype, response_type, response_size
		FROM dns_logs
	` + where + fmt.Sprintf(" ORDER BY timestamp %s LIMIT %d OFFSET %d", order, limit, offset)

	rows, err := db.DB.Query(query, args...)
	if err != nil {
		return c.JSON(fiber.Map{"error": err.Error()})
	}
	defer rows.Close()

	var results []map[string]interface{}
	for rows.Next() {
		var ts, ip, qname, rtype string
		var qtype uint16
		var size int
		rows.Scan(&ts, &ip, &qname, &qtype, &rtype, &size)
		results = append(results, map[string]interface{}{
			"timestamp":     ts,
			"client_ip":     ip,
			"domain":        qname,
			"type":          qtypeToString(qtype),
			"response_type": rtype,
			"size":          size,
		})
	}

	var total int64
	countQuery := "SELECT count() FROM dns_logs" + where
	db.DB.QueryRow(countQuery, args...).Scan(&total)

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

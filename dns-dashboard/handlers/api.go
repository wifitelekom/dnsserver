package handlers

import (
	"fmt"
	"strings"
	"time"

	"dns-dashboard/db"
	"dns-dashboard/models"

	"github.com/gofiber/fiber/v2"
)

func ApiStats(c *fiber.Ctx) error {
	stats := models.DashboardStats{}

	db.DB.QueryRow("SELECT count() FROM dns_logs").Scan(&stats.TotalQueries)
	db.DB.QueryRow("SELECT count() FROM dns_logs WHERE timestamp >= today()").Scan(&stats.TodayQueries)
	db.DB.QueryRow("SELECT uniq(client_ip) FROM dns_logs WHERE timestamp >= today()").Scan(&stats.UniqueClients)
	db.DB.QueryRow("SELECT uniq(qname) FROM dns_logs WHERE timestamp >= today()").Scan(&stats.UniqueDomains)
	db.DB.QueryRow("SELECT count() / 60.0 FROM dns_logs WHERE timestamp >= now() - INTERVAL 1 MINUTE").Scan(&stats.QPS)

	var crCount, totalCount int64
	db.DB.QueryRow("SELECT count() FROM dns_logs WHERE response_type = 'CR' AND timestamp >= today()").Scan(&crCount)
	db.DB.QueryRow("SELECT count() FROM dns_logs WHERE timestamp >= today()").Scan(&totalCount)
	if totalCount > 0 {
		stats.CacheHitRatio = float64(crCount) / float64(totalCount) * 100
	}

	return c.JSON(stats)
}

func ApiQueryTypes(c *fiber.Ctx) error {
	rows, err := db.DB.Query(`
		SELECT qtype, count() as cnt 
		FROM dns_logs 
		WHERE timestamp >= today() 
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
		rows.Scan(&s.Type, &s.Count)
		results = append(results, s)
	}
	return c.JSON(results)
}

func ApiResponseCodes(c *fiber.Ctx) error {
	rows, err := db.DB.Query(`
		SELECT response_type, count() as cnt 
		FROM dns_logs 
		WHERE timestamp >= today() 
		GROUP BY response_type 
		ORDER BY cnt DESC
	`)
	if err != nil {
		return c.JSON([]models.ResponseCodeStats{})
	}
	defer rows.Close()

	var results []models.ResponseCodeStats
	for rows.Next() {
		var s models.ResponseCodeStats
		rows.Scan(&s.Code, &s.Count)
		results = append(results, s)
	}
	return c.JSON(results)
}

func ApiTopDomains(c *fiber.Ctx) error {
	rows, err := db.DB.Query(`
		SELECT qname, count() as cnt 
		FROM dns_logs 
		WHERE timestamp >= today() AND qname != ''
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
		WHERE timestamp >= today()
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
		rows.Scan(&q.Timestamp, &q.ClientIP, &q.Domain, &q.Type, &q.ResponseType)
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
		WHERE timestamp >= now() - INTERVAL 1 HOUR
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
	responseType := strings.TrimSpace(c.Query("response_type"))
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
		where += " AND qtype = ?"
		args = append(args, qtype)
	}
	if responseType != "" {
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
		var ts, ip, qname, qtype, rtype string
		var size int
		rows.Scan(&ts, &ip, &qname, &qtype, &rtype, &size)
		results = append(results, map[string]interface{}{
			"timestamp":     ts,
			"client_ip":     ip,
			"domain":        qname,
			"type":          qtype,
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

package models

type DashboardStats struct {
	TotalQueries   int64   `json:"total_queries"`
	TodayQueries   int64   `json:"today_queries"`
	CacheHitRatio  float64 `json:"cache_hit_ratio"`
	AvgLatency     float64 `json:"avg_latency"`
	UniqueClients  int64   `json:"unique_clients"`
	UniqueDomains  int64   `json:"unique_domains"`
	QPS            float64 `json:"qps"`
	BlockedQueries int64   `json:"blocked_queries"`
}

type QueryTypeStats struct {
	Type  string `json:"type"`
	Count int64  `json:"count"`
}

type ResponseCodeStats struct {
	Code  string `json:"code"`
	Count int64  `json:"count"`
}

type TopDomain struct {
	Domain string `json:"domain"`
	Count  int64  `json:"count"`
}

type TopClient struct {
	IP    string `json:"ip"`
	Count int64  `json:"count"`
}

type RecentQuery struct {
	Timestamp    string `json:"timestamp"`
	ClientIP     string `json:"client_ip"`
	Domain       string `json:"domain"`
	Type         string `json:"type"`
	ResponseType string `json:"response_type"`
}

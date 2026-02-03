package model

// DNSLog represents a single DNS query/response event to be stored in ClickHouse.
type DNSLog struct {
	Timestamp    string `json:"timestamp"` // ClickHouse DateTime format: "2006-01-02 15:04:05"
	ClientIP     string `json:"client_ip"` // ClickHouse IPv6 format (IPv4-mapped if needed)
	QName        string `json:"qname"`
	QType        uint16 `json:"qtype"`         // numeric DNS type
	ResponseType string `json:"response_type"` // "CQ" or "CR" (Enum8 in CH)
	ResponseSize uint32 `json:"response_size"`
	RCode        uint8  `json:"rcode"` // 0..15
}

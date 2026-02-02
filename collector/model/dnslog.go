package model

import "time"

// DNSLog represents a single DNS query/response event to be stored in ClickHouse.
type DNSLog struct {
	Timestamp    time.Time `json:"timestamp"`
	ClientIP     string    `json:"client_ip"` // ClickHouse IPv6 parses from string
	QName        string    `json:"qname"`
	QType        uint16    `json:"qtype"`         // numeric DNS type
	ResponseType string    `json:"response_type"` // "CQ" or "CR" (Enum8 in CH)
	ResponseSize uint32    `json:"response_size"`
	RCode        uint8     `json:"rcode"` // 0..15
}

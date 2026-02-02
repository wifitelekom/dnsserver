CREATE TABLE IF NOT EXISTS dns.dns_logs
(
  `timestamp` DateTime,
  `client_ip` IPv6,
  `qname` String,
  INDEX idx_qname qname TYPE bloom_filter GRAMS(3) GRANULARITY 1,
  `qtype` UInt16,
  `response_type` Enum8('CQ' = 1, 'CR' = 2),
  `response_size` UInt32,
  `rcode` UInt8
)
ENGINE = MergeTree
PARTITION BY toYYYYMMDD(timestamp)
ORDER BY (timestamp, client_ip)
TTL timestamp + INTERVAL 7 DAY
SETTINGS index_granularity = 8192;

ALTER TABLE IF EXISTS dns.dns_logs
MODIFY TTL timestamp + INTERVAL 7 DAY;

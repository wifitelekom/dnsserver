CREATE TABLE IF NOT EXISTS dns.dns_logs
(
  `timestamp` DateTime,
  `client_ip` IPv6,
  `qname` LowCardinality(String),
  INDEX idx_qname qname TYPE bloom_filter GRANULARITY 4,
  INDEX idx_client client_ip TYPE minmax GRANULARITY 4,
  `qtype` UInt16,
  `response_type` Enum8('CQ' = 1, 'CR' = 2),
  `response_size` UInt32,
  `rcode` UInt8
)
ENGINE = MergeTree
PARTITION BY toYYYYMMDD(timestamp)
ORDER BY (toStartOfHour(timestamp), client_ip, qname)
TTL timestamp + INTERVAL 7 DAY
SETTINGS index_granularity = 8192,
         min_bytes_for_wide_part = 0,
         min_rows_for_wide_part = 0;

ALTER TABLE IF EXISTS dns.dns_logs
MODIFY TTL timestamp + INTERVAL 7 DAY;

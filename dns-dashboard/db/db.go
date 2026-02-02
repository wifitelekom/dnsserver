package db

import (
	"database/sql"
	"log"
	"time"

	_ "github.com/ClickHouse/clickhouse-go/v2"
)

var DB *sql.DB

func InitDB(dsn string) error {
	var err error
	// Retry connection loop
	for i := 0; i < 30; i++ {
		DB, err = sql.Open("clickhouse", dsn)
		if err == nil {
			if err = DB.Ping(); err == nil {
				return nil
			}
		}
		log.Printf("Waiting for ClickHouse... (%v)", err)
		time.Sleep(1 * time.Second)
	}
	return err
}

func CloseDB() {
	if DB != nil {
		DB.Close()
	}
}

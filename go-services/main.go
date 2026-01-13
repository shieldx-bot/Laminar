package main

import "database/sql"

func main() {
	connStr := "host=34.177.91.6 port=5432 user=postgres password=Vananh12345@ dbname=laminar sslmode=disable"
	db, err := sql.Open("postgres", connStr)
	if err != nil {
		panic(err)
	}
	db.QueryRow("
	select *  from ")
}

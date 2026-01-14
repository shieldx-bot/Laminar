package main

import (
	"github/shieldx-bot/loadbanlacing/internal/worker"
)

type VPS struct {
	IP     string
	caches []worker.ListCachesOnServer
}

var ListVPS = []VPS{
	{IP: "backend1", caches: []worker.ListCachesOnServer{}},
	{IP: "backend2", caches: []worker.ListCachesOnServer{}},
	{IP: "backend3", caches: []worker.ListCachesOnServer{}},
	{IP: "backend4", caches: []worker.ListCachesOnServer{}},
	{IP: "backend5", caches: []worker.ListCachesOnServer{}},
	{IP: "backend6", caches: []worker.ListCachesOnServer{}},
	{IP: "backend7", caches: []worker.ListCachesOnServer{}},
	{IP: "backend8", caches: []worker.ListCachesOnServer{}},
	{IP: "backend9", caches: []worker.ListCachesOnServer{}},
	{IP: "backend10", caches: []worker.ListCachesOnServer{}},
	{IP: "backend11", caches: []worker.ListCachesOnServer{}},
	{IP: "backend12", caches: []worker.ListCachesOnServer{}},
	{IP: "backend13", caches: []worker.ListCachesOnServer{}},
	{IP: "backend14", caches: []worker.ListCachesOnServer{}},
	{IP: "backend15", caches: []worker.ListCachesOnServer{}},
	{IP: "backend16", caches: []worker.ListCachesOnServer{}},
	{IP: "backend17", caches: []worker.ListCachesOnServer{}},
	{IP: "backend18", caches: []worker.ListCachesOnServer{}},
	{IP: "backend19", caches: []worker.ListCachesOnServer{}},
	{IP: "backend20", caches: []worker.ListCachesOnServer{}},
	{IP: "backend21", caches: []worker.ListCachesOnServer{}},
	{IP: "backend22", caches: []worker.ListCachesOnServer{}},
	{IP: "backend23", caches: []worker.ListCachesOnServer{}},
	{IP: "backend24", caches: []worker.ListCachesOnServer{}},
	{IP: "backend25", caches: []worker.ListCachesOnServer{}},
	{IP: "backend26", caches: []worker.ListCachesOnServer{}},
	{IP: "backend27", caches: []worker.ListCachesOnServer{}},
	{IP: "backend28", caches: []worker.ListCachesOnServer{}},
	{IP: "backend29", caches: []worker.ListCachesOnServer{}},
	{IP: "backend30", caches: []worker.ListCachesOnServer{}},
}

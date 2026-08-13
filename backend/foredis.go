package main

import (
	"fmt"
	"github.com/gomodule/redigo/redis"
	"log"
	"os"
	"sync"
	"time"
)

var dbLink redis.Conn
var usingRedis = false

var redisDial = redis.Dial
var redisSleep = time.Sleep

func init() {
	initRedis(os.Getenv("REDIS_DNS"))
}

func initRedis(redisDNS string) {
	// Check if REDIS_DNS environment variable is set
	if redisDNS == "" {
		fmt.Println("redis config not set")
		return
	}
	var err error
	for i := 0; i < 5; i++ {
		dbLink, err = redisDial("tcp", fmt.Sprintf("%s:6379", redisDNS))
		if err == nil {
			usingRedis = true
			break
		}
		log.Printf("Attempt %d: redis connection failed: %s", i+1, err)
		redisSleep(2 * time.Second)
	}

	if !usingRedis {
		log.Println("Failed to connect to redis after 5 attempts")
		return
	}

	resKeys, err := redis.Values(dbLink.Do("hkeys", "fortunes"))
	if err != nil {
		fmt.Println("redis hkeys failed", err.Error())
		return
	}

	datastoreDefault = datastore{m: map[string]fortune{}, RWMutex: &sync.RWMutex{}}
	fmt.Printf("*** loading redis fortunes:\n")
	for _, key := range resKeys {
		val, err := dbLink.Do("hget", "fortunes", key)
		if err != nil {
			fmt.Println("redis hget failed", err.Error())
		} else {
			idx := string(key.([]byte))
			msg := string(val.([]byte))
			datastoreDefault.m[idx] = fortune{ID: idx, Message: msg}
			fmt.Printf("%s => %s\n", key, val)
		}
	}
}

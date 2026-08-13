package main

import (
	"fmt"
	"github.com/gomodule/redigo/redis"
	"log"
	"os"
	"sync"
	"time"
)

var dbPool *redis.Pool
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
	
	dbPool = &redis.Pool{
		MaxIdle:     3,
		IdleTimeout: 240 * time.Second,
		Dial: func() (redis.Conn, error) {
			return redisDial("tcp", fmt.Sprintf("%s:6379", redisDNS))
		},
		TestOnBorrow: func(c redis.Conn, t time.Time) error {
			if time.Since(t) < time.Minute {
				return nil
			}
			_, err := c.Do("PING")
			return err
		},
	}

	var conn redis.Conn
	var err error
	for i := 0; i < 5; i++ {
		conn = dbPool.Get()
		if conn.Err() == nil {
			_, err = conn.Do("PING")
			if err == nil {
				usingRedis = true
				break
			}
		} else {
			err = conn.Err()
		}
		conn.Close()
		log.Printf("Attempt %d: redis connection failed: %v", i+1, err)
		redisSleep(2 * time.Second)
	}

	if !usingRedis {
		log.Println("Failed to connect to redis after 5 attempts")
		return
	}
	defer conn.Close()

	resKeys, err := redis.Values(conn.Do("hkeys", "fortunes"))
	if err != nil {
		fmt.Println("redis hkeys failed", err.Error())
		return
	}

	datastoreDefault = datastore{m: map[string]fortune{}, RWMutex: &sync.RWMutex{}}
	fmt.Printf("*** loading redis fortunes:\n")
	for _, key := range resKeys {
		val, err := conn.Do("hget", "fortunes", key)
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

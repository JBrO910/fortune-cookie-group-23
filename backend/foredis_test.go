package main

import (
	"errors"
	"github.com/gomodule/redigo/redis"
	"testing"
	"time"
)

func preserveRedisGlobals(t *testing.T) {
	t.Helper()
	prevUsingRedis := usingRedis
	prevConn := dbLink
	prevDatastore := datastoreDefault
	prevDial := redisDial
	prevSleep := redisSleep

	t.Cleanup(func() {
		usingRedis = prevUsingRedis
		dbLink = prevConn
		datastoreDefault = prevDatastore
		redisDial = prevDial
		redisSleep = prevSleep
	})
}

func TestInitRedisWithoutDNS(t *testing.T) {
	preserveRedisGlobals(t)
	usingRedis = false
	dbLink = nil

	dialCalls := 0
	redisDial = func(network, address string, options ...redis.DialOption) (redis.Conn, error) {
		dialCalls++
		return nil, nil
	}

	initRedis("")

	if usingRedis {
		t.Fatal("expected usingRedis to remain false")
	}
	if dialCalls != 0 {
		t.Fatalf("expected no dial calls, got %d", dialCalls)
	}
}

func TestInitRedisDialFailsAfterRetries(t *testing.T) {
	preserveRedisGlobals(t)
	usingRedis = false
	dbLink = nil

	dialCalls := 0
	sleepCalls := 0
	redisDial = func(network, address string, options ...redis.DialOption) (redis.Conn, error) {
		dialCalls++
		return nil, errors.New("dial failed")
	}
	redisSleep = func(d time.Duration) {
		sleepCalls++
	}

	initRedis("redis-host")

	if usingRedis {
		t.Fatal("expected usingRedis to remain false after failed retries")
	}
	if dialCalls != 5 {
		t.Fatalf("expected 5 dial attempts, got %d", dialCalls)
	}
	if sleepCalls != 5 {
		t.Fatalf("expected 5 sleep calls, got %d", sleepCalls)
	}
}

func TestInitRedisHKeysFailureKeepsStore(t *testing.T) {
	preserveRedisGlobals(t)
	usingRedis = false
	datastoreDefault = datastore{
		m: map[string]fortune{"keep": {ID: "keep", Message: "existing"}},
	}

	redisDial = func(network, address string, options ...redis.DialOption) (redis.Conn, error) {
		return &mockRedisConn{
			doFunc: func(commandName string, args ...interface{}) (interface{}, error) {
				if commandName == "hkeys" {
					return nil, errors.New("hkeys failed")
				}
				return nil, nil
			},
		}, nil
	}
	redisSleep = func(d time.Duration) {}

	initRedis("redis-host")

	if !usingRedis {
		t.Fatal("expected usingRedis to be true after successful dial")
	}
	if _, ok := datastoreDefault.m["keep"]; !ok {
		t.Fatal("expected datastore to remain unchanged when hkeys fails")
	}
}

func TestInitRedisLoadsFortunesFromRedis(t *testing.T) {
	preserveRedisGlobals(t)
	usingRedis = false

	redisDial = func(network, address string, options ...redis.DialOption) (redis.Conn, error) {
		return &mockRedisConn{
			doFunc: func(commandName string, args ...interface{}) (interface{}, error) {
				switch commandName {
				case "hkeys":
					return []interface{}{[]byte("1"), []byte("2")}, nil
				case "hget":
					key := string(args[1].([]byte))
					if key == "1" {
						return []byte("first"), nil
					}
					return nil, errors.New("missing")
				default:
					return nil, nil
				}
			},
		}, nil
	}
	redisSleep = func(d time.Duration) {}

	initRedis("redis-host")

	if !usingRedis {
		t.Fatal("expected usingRedis to be true")
	}
	if len(datastoreDefault.m) != 1 {
		t.Fatalf("expected one loaded fortune, got %d", len(datastoreDefault.m))
	}
	got, ok := datastoreDefault.m["1"]
	if !ok {
		t.Fatal("expected key 1 to be loaded")
	}
	if got.Message != "first" {
		t.Fatalf("expected message %q, got %q", "first", got.Message)
	}
}

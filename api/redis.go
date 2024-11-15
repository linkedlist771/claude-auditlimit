// api/redis.go
package api

import (
    "context"
    "log"
    "github.com/redis/go-redis/v9"
)

var (
    rdb *redis.Client
    ctx = context.Background()
)

func initRedis() {
    rdb = redis.NewClient(&redis.Options{
        Addr:     "localhost:6379", // TODO: 这里在docker1 里面需要把localhost改为redis
        Password: "", 
        DB:       0,  
    })

    _, err := rdb.Ping(ctx).Result()
    if err != nil {
        log.Fatalf("Failed to connect to Redis: %v", err)
    }
}

func incrementTokenUsage(token string) error {
    return rdb.Incr(ctx, "token:"+token).Err()
}

func getTokenUsage(token string) (int64, error) {
    return rdb.Get(ctx, "token:"+token).Int64()
}
func getAllTokenUsage() (map[string]int64, error) {
    keys, err := rdb.Keys(ctx, "token:*").Result()
    if err != nil {
        return nil, err
    }

    result := make(map[string]int64)
    for _, key := range keys {
        count, err := rdb.Get(ctx, key).Int64()
        if err != nil {
            continue // Consider whether you want to return the error instead
        }
        token := key[6:]
        result[token] = count
    }
    return result, nil  // Added nil as second return value for error
}

// api/redis.go
package api

import (
    "context"
    // "fmt"  // Added missing import
    "log"
    "strconv"
    "time"
    "github.com/redis/go-redis/v9"
)

const (
    PERIOD_3HOURS = "3h"
    PERIOD_12HOURS = "12h"
    PERIOD_24HOURS = "24h"
    PERIOD_WEEK = "1w"     // Add weekly period

    PERIOD_TOTAL = "total"
)

var (
    rdb *redis.Client
    ctx = context.Background()
)

func initRedis() {
    rdb = redis.NewClient(&redis.Options{
        Addr:     "redis:6379",
        Password: "", 
        DB:       0,  
    })

    _, err := rdb.Ping(ctx).Result()
    if err != nil {
        log.Fatalf("Failed to connect to Redis: %v", err)
    }

    // 启动定期清理任务
    go periodicCleanup()
}

func getRedisKey(token, period string) string {
    return "token:" + token + ":" + period
}

func incrementTokenUsage(token string) error {
    pipe := rdb.Pipeline()
    now := time.Now().Unix()

    // 增加总计数
    pipe.Incr(ctx, getRedisKey(token, PERIOD_TOTAL))
    
    // 增加各时间段计数并设置过期时间
    pipe.ZAdd(ctx, getRedisKey(token, PERIOD_3HOURS), redis.Z{Score: float64(now), Member: now})
    pipe.ZAdd(ctx, getRedisKey(token, PERIOD_12HOURS), redis.Z{Score: float64(now), Member: now})
    pipe.ZAdd(ctx, getRedisKey(token, PERIOD_24HOURS), redis.Z{Score: float64(now), Member: now})
    pipe.ZAdd(ctx, getRedisKey(token, PERIOD_WEEK), redis.Z{Score: float64(now), Member: now})  // Add weekly tracking

    pipe.Expire(ctx, getRedisKey(token, PERIOD_3HOURS), 3*time.Hour)
    pipe.Expire(ctx, getRedisKey(token, PERIOD_12HOURS), 12*time.Hour)
    pipe.Expire(ctx, getRedisKey(token, PERIOD_24HOURS), 24*time.Hour)
    pipe.Expire(ctx, getRedisKey(token, PERIOD_WEEK), 7*24*time.Hour)  // Set weekly expiration
    _, err := pipe.Exec(ctx)
    return err
}

type TokenUsageStats struct {
    Total      int64 `json:"total"`
    Last3Hours int64 `json:"last_3_hours"`
    Last12Hours int64 `json:"last_12_hours"`
    Last24Hours int64 `json:"last_24_hours"`
    LastWeek    int64 `json:"last_week"`    // Add weekly stats

}

func getTokenUsage(token string) (*TokenUsageStats, error) {
    now := time.Now().Unix()
    threeHoursAgo := now - 3*3600
    twelveHoursAgo := now - 12*3600
    twentyFourHoursAgo := now - 24*3600
    weekAgo := now - 7*24*3600    // Add week calculation

    pipe := rdb.Pipeline()
    
    totalCmd := pipe.Get(ctx, getRedisKey(token, PERIOD_TOTAL))
    threeHoursCmd := pipe.ZCount(ctx, getRedisKey(token, PERIOD_3HOURS), strconv.FormatInt(threeHoursAgo, 10), "+inf")
    twelveHoursCmd := pipe.ZCount(ctx, getRedisKey(token, PERIOD_12HOURS), strconv.FormatInt(twelveHoursAgo, 10), "+inf")
    twentyFourHoursCmd := pipe.ZCount(ctx, getRedisKey(token, PERIOD_24HOURS), strconv.FormatInt(twentyFourHoursAgo, 10), "+inf")
    weekCmd := pipe.ZCount(ctx, getRedisKey(token, PERIOD_WEEK), strconv.FormatInt(weekAgo, 10), "+inf")  // Add weekly count

    _, err := pipe.Exec(ctx)
    if err != nil && err != redis.Nil {
        return nil, err
    }

    total, _ := totalCmd.Int64()
    threeHours, _ := threeHoursCmd.Result()
    twelveHours, _ := twelveHoursCmd.Result()
    twentyFourHours, _ := twentyFourHoursCmd.Result()
    week, _ := weekCmd.Result()    // Get weekly result

    return &TokenUsageStats{
        Total:       total,
        Last3Hours:  threeHours,
        Last12Hours: twelveHours,
        Last24Hours: twentyFourHours,
        LastWeek:    week,         // Include weekly stats

    }, nil
}

func getAllTokenUsage() (map[string]*TokenUsageStats, error) {
    keys, err := rdb.Keys(ctx, "token:*:total").Result()
    if err != nil {
        return nil, err
    }

    result := make(map[string]*TokenUsageStats)
    for _, key := range keys {
        token := key[6 : len(key)-6] // 移除 "token:" 前缀和 ":total" 后缀
        stats, err := getTokenUsage(token)
        if err != nil {
            continue
        }
        result[token] = stats
    }
    return result, nil
}

func periodicCleanup() {
    for {
        now := time.Now().Unix()
        threeHoursAgo := now - 3*3600
        twelveHoursAgo := now - 12*3600
        twentyFourHoursAgo := now - 24*3600
        weekAgo := now - 7*24*3600    // Add week cleanup time

        keys, err := rdb.Keys(ctx, "token:*:[3|12|24]h|1w").Result()    // Include weekly keys
        if err != nil {
            time.Sleep(5 * time.Minute)
            continue
        }

        for _, key := range keys {
            if key[len(key)-2:] == "3h" {
                rdb.ZRemRangeByScore(ctx, key, "-inf", strconv.FormatInt(threeHoursAgo, 10))
            } else if key[len(key)-3:] == "12h" {
                rdb.ZRemRangeByScore(ctx, key, "-inf", strconv.FormatInt(twelveHoursAgo, 10))
            } else if key[len(key)-3:] == "24h" {
                rdb.ZRemRangeByScore(ctx, key, "-inf", strconv.FormatInt(twentyFourHoursAgo, 10))
            } else if key[len(key)-2:] == "1w" {    // Add weekly cleanup
                rdb.ZRemRangeByScore(ctx, key, "-inf", strconv.FormatInt(weekAgo, 10))
            }
        }
        time.Sleep(5 * time.Minute)
    }
}

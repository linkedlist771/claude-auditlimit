// api/stats.go
package api

import (
    "sort"
    "time"
    "github.com/gogf/gf/v2/frame/g"
    "github.com/gogf/gf/v2/net/ghttp"
)

type TokenStats struct {
    Token           string `json:"token"`
    TotalUsage      int64  `json:"total_usage"`
    CurrentActive   bool   `json:"current_active"`
    LastSeenSeconds int64  `json:"last_seen_seconds,omitempty"`
}

func GetTokenStats(r *ghttp.Request) {
    mtx.Lock()
    defer mtx.Unlock()

    usageStats, err := getAllTokenUsage()
    if err != nil {
        r.Response.WriteJsonExit(g.Map{
            "code": 500,
            "msg":  "Failed to get token statistics",
            "error": err.Error(),
        })
        return
    }

    stats := make([]TokenStats, 0)
    now := time.Now()

    for token, usage := range usageStats {
        stat := TokenStats{
            Token:      token,
            TotalUsage: usage,
        }

        if v, exists := visitors[token]; exists {
            stat.CurrentActive = true
            stat.LastSeenSeconds = int64(now.Sub(v.lastSeen).Seconds())
        }

        stats = append(stats, stat)
    }

    for token, v := range visitors {
        if _, exists := usageStats[token]; !exists {
            stats = append(stats, TokenStats{
                Token:           token,
                TotalUsage:     0,
                CurrentActive:   true,
                LastSeenSeconds: int64(now.Sub(v.lastSeen).Seconds()),
            })
        }
    }

    sort.Slice(stats, func(i, j int) bool {
        return stats[i].TotalUsage > stats[j].TotalUsage
    })

    r.Response.WriteJsonExit(g.Map{
        "code": 0,
        "msg":  "success",
        "data": stats,
    })
}

package api

import (
    "crypto/sha256"
    "encoding/hex"
    "fmt"
    "sort"
    "time"
    "github.com/gogf/gf/v2/frame/g"
    "github.com/gogf/gf/v2/net/ghttp"
	"auditlimit/config"

)

var (
    MAX_DEVICES = int64(config.MAX_DEVICES) // Convert to int64
    DEVICE_EXPIRE = 7 * 24 * time.Hour // 默认7天过期
)

// DeviceInfo 存储设备信息
type DeviceInfo struct {
    UserAgent string `json:"user_agent"`
    Host      string `json:"host"`
}

// generateDeviceHash creates a unique hash for the device
func generateDeviceHash(identifier string) string {
    hasher := sha256.New()
    hasher.Write([]byte(identifier))
    return hex.EncodeToString(hasher.Sum(nil))
}

// getDeviceKey returns Redis key for storing device information
func getDeviceKey(token string) string {
    return fmt.Sprintf("devices:%s", token)
}

// 修改函数签名，添加所需参数
func checkAndAddDevice(token string, deviceIdentifier string, userAgent string, host string) (bool, error) {
    deviceHash := generateDeviceHash(deviceIdentifier)
    key := getDeviceKey(token)

    exists, err := rdb.SIsMember(ctx, key, deviceHash).Result()
   // Use structured logging with JSON format
   g.Log().Debug(ctx, "Device check", g.Map{
    "token": token,
    "deviceHash": deviceHash,
    "userAgent": userAgent,
    "host": host,
    "key": key,
    "exists": exists,
})


    if err != nil {
        return false, err
    }

    if exists {
        return true, nil
    }

    count, err := rdb.SCard(ctx, key).Result()
    if err != nil {
        return false, err
    }

    if count >= MAX_DEVICES {
        return false, nil
    }

    err = rdb.SAdd(ctx, key, deviceHash).Err()
    if err != nil {
        return false, err
    }
    
    // 创建设备信息
    info := &DeviceInfo{
        UserAgent: userAgent,
        Host:      host,
    }
    
    // 存储设备信息
    err = storeDeviceInfo(token, deviceHash, info)
    if err != nil {
        return false, err
    }

    err = rdb.Expire(ctx, key, DEVICE_EXPIRE).Err()
    return true, err
}

// GetDeviceList 获取当前token注册的所有设备
func GetDeviceList(r *ghttp.Request) {
    token := r.Header.Get("Authorization")
    if token != "" {
        token = token[7:] // Remove "Bearer "
    }

    key := getDeviceKey(token)
    
    // 获取所有设备hash
    deviceHashes, err := rdb.SMembers(ctx, key).Result()
    if err != nil {
        r.Response.WriteJsonExit(g.Map{
            "code": 500,
            "msg":  "Failed to get device list",
            "error": err.Error(),
        })
        return
    }

    // 获取设备信息的详细列表
    deviceList := make([]*DeviceInfo, 0)
    
    for _, hash := range deviceHashes {
        // 从hash反向获取设备信息
        // 这里我们需要另外存储设备信息的详细数据
        deviceInfoKey := fmt.Sprintf("device_info:%s:%s", token, hash)
        userAgent, err := rdb.HGet(ctx, deviceInfoKey, "user_agent").Result()
        if err != nil {
            continue
        }
        host, err := rdb.HGet(ctx, deviceInfoKey, "host").Result()
        if err != nil {
            continue
        }
        
        deviceList = append(deviceList, &DeviceInfo{
            UserAgent: userAgent,
            Host:      host,
        })
    }

    r.Response.WriteJsonExit(g.Map{
        "code": 0,
        "msg":  "Success",
        "data": g.Map{
            "devices": deviceList,
            "total":   len(deviceList),
        },
    })
}

// DeviceLogout handles device logout
func DeviceLogout(r *ghttp.Request) {
    token := r.Header.Get("Authorization")
    if token != "" {
        token = token[7:] // Remove "Bearer "
    }

	host := r.Host
    userAgent := r.Header.Get("User-Agent")
    
    if host == "" || userAgent == "" {
        r.Response.WriteJsonExit(g.Map{
            "code": 400,
            "msg":  "Host and User-Agent are required",
        })
        return
    }
    
    deviceIdentifier := userAgent //fmt.Sprintf("%s:%s", userAgent, host)

    if deviceIdentifier == "" {
        r.Response.WriteJsonExit(g.Map{
            "code": 400,
            "msg":  "device_identifier is required",
        })
        return
    }

    deviceHash := generateDeviceHash(deviceIdentifier)
    key := getDeviceKey(token)

    // 删除设备信息
    deviceInfoKey := fmt.Sprintf("device_info:%s:%s", token, deviceHash)
    err := rdb.Del(ctx, deviceInfoKey).Err()
    if err != nil {
        r.Response.WriteJsonExit(g.Map{
            "code": 500,
            "msg":  "Failed to remove device info",
            "error": err.Error(),
        })
        return
    }

    // Remove device from set
    err = rdb.SRem(ctx, key, deviceHash).Err()
    if err != nil {
        r.Response.WriteJsonExit(g.Map{
            "code": 500,
            "msg":  "Failed to logout device",
            "error": err.Error(),
        })
        return
    }

    r.Response.WriteJsonExit(g.Map{
        "code": 0,
        "msg":  "Device logged out successfully",
    })
}

func storeDeviceInfo(token, deviceHash string, info *DeviceInfo) error {
    deviceInfoKey := fmt.Sprintf("device_info:%s:%s", token, deviceHash)
    
    err := rdb.HSet(ctx, deviceInfoKey, map[string]interface{}{
        "user_agent": info.UserAgent,
        "host":       info.Host,
    }).Err()
    
    if err != nil {
        return err
    }
    
    return rdb.Expire(ctx, deviceInfoKey, DEVICE_EXPIRE).Err()
}



// TokenDeviceStats represents statistics about a token's devices
type TokenDeviceStats struct {
    Token    string        `json:"token"`
    Devices  []*DeviceInfo `json:"devices"`
    Total    int          `json:"total"`
}

// GetAllTokenDevices returns device information for all tokens
func GetAllTokenDevices(r *ghttp.Request) {
    // Get all keys matching the pattern "devices:*"
    keys, err := rdb.Keys(ctx, "devices:*").Result()
    if err != nil {
        r.Response.WriteJsonExit(g.Map{
            "code": 500,
            "msg":  "Failed to get token keys",
            "error": err.Error(),
        })
        return
    }

    // Create stats for each token
    allStats := make([]TokenDeviceStats, 0)
    
    for _, key := range keys {
        // Extract token from key (remove "devices:" prefix)
        token := key[8:] // len("devices:") = 8
        
        // Get device hashes for this token
        deviceHashes, err := rdb.SMembers(ctx, key).Result()
        if err != nil {
            continue
        }

        // Get device info for each hash
        deviceList := make([]*DeviceInfo, 0)
        for _, hash := range deviceHashes {
            deviceInfoKey := fmt.Sprintf("device_info:%s:%s", token, hash)
            userAgent, err := rdb.HGet(ctx, deviceInfoKey, "user_agent").Result()
            if err != nil {
                continue
            }
            host, err := rdb.HGet(ctx, deviceInfoKey, "host").Result()
            if err != nil {
                continue
            }
            
            deviceList = append(deviceList, &DeviceInfo{
                UserAgent: userAgent,
                Host:      host,
            })
        }

        // Add stats for this token
        stats := TokenDeviceStats{
            Token:   token,
            Devices: deviceList,
            Total:   len(deviceList),
        }
        
        allStats = append(allStats, stats)
    }

    // Sort by total number of devices (optional)
    sort.Slice(allStats, func(i, j int) bool {
        return allStats[i].Total > allStats[j].Total
    })

    r.Response.WriteJsonExit(g.Map{
        "code": 0,
        "msg":  "Success",
        "data": allStats,
    })
}

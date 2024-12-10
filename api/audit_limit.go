// api/audit_limit.go

package api

import (
	"auditlimit/config"
	"strconv"
	"strings"
	"time"

	// "fmt"
	"github.com/gogf/gf/v2/encoding/gjson"
	"github.com/gogf/gf/v2/frame/g"
	"github.com/gogf/gf/v2/net/ghttp"
	"github.com/gogf/gf/v2/text/gstr"
	"github.com/gogf/gf/v2/util/gconv"
)

func AuditLimit(r *ghttp.Request) {
	ctx := r.Context()
	// 获取Bearer Token 用来判断用户身份
	token := r.Header.Get("Authorization")
	// 移除Bearer
	if token != "" {
		token = token[7:]
	}
	// Add device identifier check

	// host := r.Header.Get("Host")
	host := r.Header.Get("X-Forwarded-Host")
	if host == "" {
		host = r.Host // 如果没有X-Forwarded-Host，则使用直接的Host
	}
	userAgent := r.Header.Get("User-Agent")
	g.Log().Debug(ctx, "AuditLimit host", host)
	g.Log().Debug(ctx, "AuditLimit userAgent", userAgent)

	if host == "" || userAgent == "" {
		r.Response.WriteJsonExit(g.Map{
			"code": 400,
			"msg":  "Host and User-Agent are required",
		})
		return
	}

	deviceIdentifier := userAgent //fmt.Sprintf("%s:%s", host, userAgent)

	if deviceIdentifier == "" {
		r.Response.Status = 400
		r.Response.WriteJson(g.Map{
			"error": g.Map{
				"message": "Device identifier is required" + "\n" + "设备标识符是必需的",
			},
		})
		return
	}

	// Check device authorization
	allowed, err := checkAndAddDevice(token, deviceIdentifier, userAgent, host)
	if err != nil {
		g.Log().Error(ctx, "Device check failed", err)
		r.Response.Status = 500
		r.Response.WriteJson(g.Map{
			"error": g.Map{
				"message": "Failed to verify device" + "\n" + "无法验证设备",
			},
		})
		return
	}

	if !allowed {
		r.Response.Status = 403
		r.Response.WriteJson(g.Map{
			"error": g.Map{
				"message": "Maximum number of devices (" + strconv.Itoa(config.MAX_DEVICES) + ") reached. Please logout from another device first." + "\n" + "已达到最大设备数 (" + strconv.Itoa(config.MAX_DEVICES) + ")。请先从另一台设备注销。",
			},
		})
		return
	}

	g.Log().Debug(ctx, "token", token)
	// 获取gfsessionid 可以用来分析用户是否多设备登录

	gfsessionid := r.Cookie.Get("gfsessionid").String()
	g.Log().Debug(ctx, "gfsessionid", gfsessionid)
	// 获取referer 可以用来判断用户请求来源
	referer := r.Header.Get("referer")
	g.Log().Debug(ctx, "referer", referer)
	// 获取请求内容
	reqJson, err := r.GetJson()
	if err != nil {
		g.Log().Error(ctx, "GetJson", err)
		r.Response.Status = 400
		r.Response.WriteJson(g.Map{
			"error": g.Map{
				"message": err.Error(),
			},
		})
	}
	action := reqJson.Get("action").String() // action为 next时才是真正的请求，否则可能是继续上次请求 action 为 variant 时为重新生成
	g.Log().Debug(ctx, "action", action)

	model := reqJson.Get("model").String() // 模型名称
	g.Log().Debug(ctx, "model", model)
	prompt := reqJson.Get("messages.0.content.parts.0").String() // 输入内容
	g.Log().Debug(ctx, "prompt", prompt)

	// 判断提问内容是否包含禁止词
	if containsAny(ctx, prompt, config.ForbiddenWords) {
		r.Response.Status = 400
		r.Response.WriteJson(g.Map{
			"error": g.Map{
				"message": "请珍惜账号,不要提问违禁内容.",
			},
		})
		return
	}

	// OPENAI Moderation 检测
	if config.OAIKEY != "" && config.MODERATION != "" {
		// 检测是否包含违规内容
		respVar := g.Client().SetHeaderMap(g.MapStrStr{
			"Authorization": "Bearer " + config.OAIKEY,
			"Content-Type":  "application/json",
		}).PostVar(ctx, config.MODERATION, g.Map{
			"input": prompt,
		})

		// 返回的 json 中 results.flagged 为 true 时为违规内容
		// respBody := resp.ReadAllString()
		//g.Log().Debug(ctx, "resp:", respBody)
		g.Dump(respVar)
		respJson := gjson.New(respVar)
		isFlagged := respJson.Get("results.0.flagged").Bool()
		g.Log().Debug(ctx, "flagged", isFlagged)
		if isFlagged {
			r.Response.Status = 400
			r.Response.WriteJson(MsgMod400)
			return
		}
	}

	// 判断模型是否为plus模型 如果是则使用plus模型的限制
	if gstr.HasPrefix(model, "claude") {
		// 在if gstr.HasPrefix(model, "claude")内部替换原有代码
		stats, err := getTokenUsage(token)
		if err != nil {
			g.Log().Error(ctx, "Failed to get token usage", err)
			r.Response.Status = 500
			r.Response.WriteJson(g.Map{
				"error": g.Map{
					"message": "Failed to check usage limits",
				},
			})
			return
		}

		// 获取3小时内的使用次数
		used3h := stats.Last3Hours
		// 计算剩余次数
		remain := int64(config.LIMIT) - used3h

		g.Log().Debug(ctx, "3h usage", used3h)
		g.Log().Debug(ctx, "remain", remain)

		if remain <= 0 {
			r.Response.Status = 429

			// Get TTL of the 3-hour window key
			key := getRedisKey(token, PERIOD_3HOURS)
			ttl, err := rdb.TTL(ctx, key).Result()
			if err != nil {
				g.Log().Error(ctx, "Failed to get TTL", err)
				// Default to maximum wait time if error
				ttl = 3 * time.Hour
			}

			// Convert TTL to seconds
			wait := int64(ttl.Seconds())
			if wait < 0 {
				// If key doesn't exist or has no TTL, set to 0
				wait = 0
			}

			g.Log().Debug(ctx, "wait seconds", wait)

			r.Response.WriteJson(g.Map{
				"error": g.Map{
					"message": "You have triggered the usage frequency limit, the current limit is " +
						gconv.String(config.LIMIT) + " times/3h, please wait " +
						gconv.String(wait) + " seconds before trying again.\n" +
						"您已经触发使用频率限制,当前限制为 " + gconv.String(config.LIMIT) +
						" 次/3小时,请等待 " + gconv.String(wait) + " 秒后再试.",
				},
			})
			return
		} else {
			// 记录本次使用
			err := incrementTokenUsage(token)
			if err != nil {
				g.Log().Error(ctx, "Failed to increment usage", err)
			}
			r.Response.Status = 200
			return
		}

		_ = GetVisitor(token, config.LIMIT, config.PER)
		// 获取剩余次数
		// remain := limiter.TokensAt(time.Now())
		// g.Log().Debug(ctx, "remain", remain)
		// if remain < 1 {
		// 	r.Response.Status = 429
		// 	// resMsg := gjson.New(MsgPlus429)
		// 	// 根据remain计算需要等待的时间
		// 	// 生产间隔
		// 	creatInterval := config.PER / time.Duration(config.LIMIT)
		// 	// 转换为秒
		// 	creatIntervalSec := float64(creatInterval.Seconds())
		// 	// 等待时间
		// 	wait := (1 - remain) * creatIntervalSec
		// 	g.Log().Debug(ctx, "wait", wait, "creatIntervalSec", creatIntervalSec)
		// 	r.Response.WriteJson(g.Map{
		// 		"error": g.Map{
		// 			"message": "You have triggered the usage frequency limit, the current limit is " + gconv.String(config.LIMIT) + " times/" + gconv.String(config.PER) + ", please wait " + gconv.String(int(wait)) + " seconds before trying again.\n" + "您已经触发使用频率限制,当前限制为 " + gconv.String(config.LIMIT) + " 次/" + gconv.String(config.PER) + ",请等待 " + gconv.String(int(wait)) + " 秒后再试.",
		// 		},
		// 	})
		// 	return
		// } else {
		// 	// 消耗一个令牌
		// 	limiter.Allow()
		// 	r.Response.Status = 200
		// 	return
		// }

	}

	r.Response.Status = 200

}

// 判断字符串是否包含数组中的任意一个元素
func containsAny(ctx g.Ctx, text string, array []string) bool {
	for _, item := range array {
		if strings.Contains(text, item) {
			g.Log().Debug(ctx, "containsAny", text, item)
			return true
		}
	}
	return false
}

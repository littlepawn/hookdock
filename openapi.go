package main

import (
	"encoding/json"
	"net/http"
)

var openapiDoc = map[string]any{
	"openapi": "3.1.0",
	"info": map[string]any{
		"title":       "hookdock API",
		"version":     "0.2.0",
		"description": "Webhook 接收 + 规则触发定时任务的网关。POST 任意 body 到 /hook/{slug} 即可留档，命中规则时自动生成可重试的定时任务；任务成功或重试用尽后可推送到 Bark、企业微信、钉钉、飞书、Telegram、Slack、Discord 等渠道。",
	},
	"servers": []map[string]any{{"url": "/", "description": "当前实例"}},
	"paths": map[string]any{
		"/hook/{slug}": map[string]any{
			"post": map[string]any{
				"summary":     "接收 webhook",
				"operationId": "receiveHook",
				"tags":        []string{"webhook"},
				"parameters": []map[string]any{
					{"name": "slug", "in": "path", "required": true, "schema": map[string]any{"type": "string"}},
					{"name": "X-Api-Token", "in": "header", "required": false, "schema": map[string]any{"type": "string"},
						"description": "服务端建过令牌后必填。令牌需具备 hook.send 权限，且作用域覆盖该端点。"},
					{"name": "X-Hook-Job", "in": "header", "required": false, "schema": map[string]any{"type": "string"},
						"description": "可选。一行 JSON 任务指令，让本条请求顺带创建任务；需要令牌具备 hook.trigger 权限。字段同 body.__job__。"},
				},
				"requestBody": map[string]any{
					"required": false,
					"content": map[string]any{
						"application/json": map[string]any{"schema": map[string]any{"type": "object",
							"properties": map[string]any{
								"__job__": map[string]any{
									"type":        "object",
									"description": "可选。附带任务指令，替代 X-Hook-Job 头。需要 hook.trigger 权限。",
									"properties": map[string]any{
										"name":         map[string]any{"type": "string"},
										"schedule":     map[string]any{"type": "string", "description": "5 段 cron 或 @every 30s；默认 @every 5m"},
										"exec_argv":    map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
										"timeout_sec":  map[string]any{"type": "integer", "minimum": 1, "maximum": 3600},
										"max_attempts": map[string]any{"type": "integer", "minimum": 1, "maximum": 10},
										"payload":      map[string]any{"type": "string", "description": "覆盖给任务的 payload，默认用本次请求的 payload"},
										"oneshot":      map[string]any{"type": "boolean", "description": "true=只执行一次后自动停用"},
									},
									"required": []string{"exec_argv"},
								},
							},
						}},
						"text/plain":                        map[string]any{"schema": map[string]any{"type": "string"}},
						"application/x-www-form-urlencoded": map[string]any{"schema": map[string]any{"type": "object"}},
					},
				},
				"responses": map[string]any{
					"200": map[string]any{
						"description": "已接收并留档",
						"content": map[string]any{"application/json": map[string]any{"schema": map[string]any{
							"type": "object",
							"properties": map[string]any{
								"ok":           map[string]any{"type": "boolean"},
								"request_id":   map[string]any{"type": "string"},
								"endpoint":     map[string]any{"type": "string"},
								"matched_jobs": map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "规则命中 + 内联指令创建的全部任务 id"},
								"rule_jobs":    map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "仅规则命中创建的"},
								"inline_jobs":  map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "仅 X-Hook-Job / __job__ 指令创建的"},
								"received_at":  map[string]any{"type": "integer"},
							},
						}}},
					},
					"401": map[string]any{"description": "鉴权失败（令牌缺失/无效，或端点 token / HMAC 校验失败）"},
					"403": map[string]any{"description": "令牌缺少 hook.send / hook.trigger 权限，或作用域不含该端点"},
					"404": map[string]any{"description": "端点不存在或已停用"},
					"429": map[string]any{"description": "超过速率限制"},
				},
			},
			"get": map[string]any{
				"summary":     "接收 webhook（GET 形式，常用于探活与简单触发）",
				"operationId": "receiveHookGet",
				"tags":        []string{"webhook"},
				"parameters": []map[string]any{
					{"name": "slug", "in": "path", "required": true, "schema": map[string]any{"type": "string"}},
				},
				"responses": map[string]any{"200": map[string]any{"description": "已接收"}},
			},
		},
		"/healthz": map[string]any{
			"get": map[string]any{
				"summary": "健康检查", "operationId": "healthz", "tags": []string{"ops"},
				"responses": map[string]any{"200": map[string]any{"description": "ok"}},
			},
		},
		"/api/overview": map[string]any{
			"get": map[string]any{
				"summary": "概览统计", "operationId": "overview", "tags": []string{"admin"},
				"security":  []map[string]any{{"AdminToken": []string{}}},
				"responses": map[string]any{"200": map[string]any{"description": "统计 + 端点 + 最近请求/运行"}},
			},
		},
		"/api/endpoints": map[string]any{
			"get": map[string]any{
				"summary": "端点列表", "operationId": "listEndpoints", "tags": []string{"admin"},
				"security":  []map[string]any{{"AdminToken": []string{}}},
				"responses": map[string]any{"200": map[string]any{"description": "端点数组"}},
			},
			"post": map[string]any{
				"summary": "新建端点", "operationId": "createEndpoint", "tags": []string{"admin"},
				"security":  []map[string]any{{"AdminToken": []string{}}},
				"responses": map[string]any{"200": map[string]any{"description": "已创建的端点"}},
			},
		},
		"/api/endpoints/{key}/requests": map[string]any{
			"get": map[string]any{
				"summary": "端点收到的请求", "operationId": "listEndpointRequests", "tags": []string{"admin"},
				"security": []map[string]any{{"AdminToken": []string{}}},
				"parameters": []map[string]any{
					{"name": "key", "in": "path", "required": true, "schema": map[string]any{"type": "string"}, "description": "slug 或 id"},
					{"name": "limit", "in": "query", "schema": map[string]any{"type": "integer", "default": 50}},
					{"name": "before", "in": "query", "schema": map[string]any{"type": "integer"}, "description": "游标：只看该时间戳之前"},
				},
				"responses": map[string]any{"200": map[string]any{"description": "请求数组"}},
			},
		},
		"/api/endpoints/{key}/rules": map[string]any{
			"get": map[string]any{
				"summary": "端点规则列表", "operationId": "listRules", "tags": []string{"admin"},
				"security":  []map[string]any{{"AdminToken": []string{}}},
				"responses": map[string]any{"200": map[string]any{"description": "规则数组"}},
			},
			"post": map[string]any{
				"summary": "新增规则", "operationId": "createRule", "tags": []string{"admin"},
				"security": []map[string]any{{"AdminToken": []string{}}},
				"requestBody": map[string]any{
					"required": true,
					"content": map[string]any{
						"application/json": map[string]any{"schema": map[string]any{"$ref": "#/components/schemas/RuleInput"}},
					},
				},
				"responses": map[string]any{"200": map[string]any{"description": "已创建的规则"}},
			},
		},
		"/api/requests/{id}": map[string]any{
			"get": map[string]any{
				"summary": "请求详情", "operationId": "getRequest", "tags": []string{"admin"},
				"security":  []map[string]any{{"AdminToken": []string{}}},
				"responses": map[string]any{"200": map[string]any{"description": "完整请求（body/headers/payload）"}},
			},
		},
		"/api/requests/{id}/make-job": map[string]any{
			"post": map[string]any{
				"summary": "把一条请求转成定时任务", "operationId": "requestToJob", "tags": []string{"admin"},
				"security": []map[string]any{{"AdminToken": []string{}}},
				"requestBody": map[string]any{
					"required": true,
					"content": map[string]any{
						"application/json": map[string]any{"schema": map[string]any{"$ref": "#/components/schemas/JobInput"}},
					},
				},
				"responses": map[string]any{"200": map[string]any{"description": "已创建的任务"}},
			},
		},
		"/api/jobs": map[string]any{
			"get": map[string]any{
				"summary": "任务列表", "operationId": "listJobs", "tags": []string{"admin"},
				"security":  []map[string]any{{"AdminToken": []string{}}},
				"responses": map[string]any{"200": map[string]any{"description": "任务数组"}},
			},
			"post": map[string]any{
				"summary": "新建任务", "operationId": "createJob", "tags": []string{"admin"},
				"security": []map[string]any{{"AdminToken": []string{}}},
				"requestBody": map[string]any{
					"required": true,
					"content": map[string]any{
						"application/json": map[string]any{"schema": map[string]any{"$ref": "#/components/schemas/JobInput"}},
					},
				},
				"responses": map[string]any{"200": map[string]any{"description": "已创建的任务"}},
			},
		},
		"/api/jobs/{id}/run": map[string]any{
			"post": map[string]any{
				"summary": "立即执行一次", "operationId": "runJob", "tags": []string{"admin"},
				"security":  []map[string]any{{"AdminToken": []string{}}},
				"responses": map[string]any{"200": map[string]any{"description": "已入队"}},
			},
		},
		"/api/jobs/{id}/toggle": map[string]any{
			"post": map[string]any{
				"summary": "启用 / 暂停", "operationId": "toggleJob", "tags": []string{"admin"},
				"security":  []map[string]any{{"AdminToken": []string{}}},
				"responses": map[string]any{"200": map[string]any{"description": "当前启用状态"}},
			},
		},
		"/api/channels/types": map[string]any{
			"get": map[string]any{
				"summary": "推送渠道类型目录", "operationId": "channelTypes", "tags": []string{"channels"},
				"security":  []map[string]any{{"ApiToken": []string{}}},
				"responses": map[string]any{"200": map[string]any{"description": "渠道类型与各类型配置字段"}},
			},
		},
		"/api/channels": map[string]any{
			"get": map[string]any{
				"summary": "推送渠道列表", "operationId": "listChannels", "tags": []string{"channels"},
				"security": []map[string]any{{"ApiToken": []string{}}},
				"responses": map[string]any{"200": map[string]any{
					"description": "渠道数组。无 channel.manage 权限时 config 字段为空。",
				}},
			},
			"post": map[string]any{
				"summary": "新建推送渠道", "operationId": "createChannel", "tags": []string{"channels"},
				"security": []map[string]any{{"ApiToken": []string{}}},
				"requestBody": map[string]any{
					"required": true,
					"content": map[string]any{"application/json": map[string]any{"schema": map[string]any{"$ref": "#/components/schemas/ChannelInput"}}},
				},
				"responses": map[string]any{
					"200": map[string]any{"description": "已创建的渠道"},
					"400": map[string]any{"description": "类型未知或必填配置缺失"},
				},
			},
		},
		"/api/channels/{id}": map[string]any{
			"get": map[string]any{
				"summary": "推送渠道详情", "operationId": "getChannel", "tags": []string{"channels"},
				"security":  []map[string]any{{"ApiToken": []string{}}},
				"responses": map[string]any{"200": map[string]any{"description": "渠道详情"}},
			},
			"put": map[string]any{
				"summary": "修改推送渠道", "operationId": "updateChannel", "tags": []string{"channels"},
				"security":  []map[string]any{{"ApiToken": []string{}}},
				"responses": map[string]any{"200": map[string]any{"description": "更新后的渠道"}},
			},
			"delete": map[string]any{
				"summary": "删除推送渠道", "operationId": "deleteChannel", "tags": []string{"channels"},
				"security":  []map[string]any{{"ApiToken": []string{}}},
				"responses": map[string]any{"200": map[string]any{"description": "删除成功"}},
			},
		},
		"/api/channels/{id}/test": map[string]any{
			"post": map[string]any{
				"summary": "发送测试消息", "operationId": "testChannel", "tags": []string{"channels"},
				"security": []map[string]any{{"ApiToken": []string{}}},
				"responses": map[string]any{
					"200": map[string]any{"description": "渠道接受消息"},
					"502": map[string]any{"description": "渠道返回错误，error 字段是原始错误（不含密钥）"},
				},
			},
		},
		"/api/runs": map[string]any{
			"get": map[string]any{
				"summary": "运行记录", "operationId": "listRuns", "tags": []string{"admin"},
				"security": []map[string]any{{"AdminToken": []string{}}},
				"parameters": []map[string]any{
					{"name": "job", "in": "query", "schema": map[string]any{"type": "string"}},
					{"name": "status", "in": "query", "schema": map[string]any{"type": "string"}},
					{"name": "limit", "in": "query", "schema": map[string]any{"type": "integer", "default": 50}},
				},
				"responses": map[string]any{"200": map[string]any{"description": "运行数组"}},
			},
		},
		"/api/runs/{id}/retry": map[string]any{
			"post": map[string]any{
				"summary": "死信重投", "operationId": "retryRun", "tags": []string{"admin"},
				"security":  []map[string]any{{"AdminToken": []string{}}},
				"responses": map[string]any{"200": map[string]any{"description": "已重新入队"}},
			},
		},
		"/api/whoami": map[string]any{
			"get": map[string]any{
				"summary": "当前令牌身份与权限",
				"tags":    []string{"tokens"},
				"responses": map[string]any{
					"200": map[string]any{"description": "身份、权限清单、角色预设"},
					"401": map[string]any{"description": "未携带令牌"},
				},
			},
		},
		"/api/tokens": map[string]any{
			"get": map[string]any{
				"summary":   "列出令牌",
				"tags":      []string{"tokens"},
				"security":  []map[string][]string{{"ApiToken": {}}},
				"responses": map[string]any{"200": map[string]any{"description": "令牌列表（不含明文与 hash）"}},
			},
			"post": map[string]any{
				"summary":  "创建令牌",
				"tags":     []string{"tokens"},
				"security": []map[string][]string{{"ApiToken": {}}},
				"requestBody": map[string]any{
					"required": true,
					"content": map[string]any{"application/json": map[string]any{"schema": map[string]any{
						"type": "object",
						"properties": map[string]any{
							"name":   map[string]any{"type": "string", "description": "访问对象，如「CI 机器人」「张三-只读」"},
							"role":   map[string]any{"type": "string", "enum": []string{"sender", "trigger", "operator", "admin"}, "description": "角色预设；与 perms 同时给出时 perms 优先"},
							"perms":  map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "细粒度权限，见 GET /api/whoami 的 all_perms"},
							"scopes": map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "限定可访问的端点 slug；空数组=全部"},
							"note":   map[string]any{"type": "string"},
						},
						"required": []string{"name"},
					}}},
				},
				"responses": map[string]any{"200": map[string]any{"description": "创建成功，响应里的 plain 字段是明文令牌，只显示这一次"}},
			},
		},
		"/api/tokens/{id}": map[string]any{
			"put": map[string]any{
				"summary":   "修改令牌（名称/角色/权限/作用域/启停）",
				"tags":      []string{"tokens"},
				"security":  []map[string][]string{{"ApiToken": {}}},
				"responses": map[string]any{"200": map[string]any{"description": "更新后的令牌"}},
			},
			"delete": map[string]any{
				"summary":   "删除令牌",
				"tags":      []string{"tokens"},
				"security":  []map[string][]string{{"ApiToken": {}}},
				"responses": map[string]any{"200": map[string]any{"description": "删除成功"}},
			},
		},
		"/api/stream": map[string]any{
			"get": map[string]any{
				"summary": "SSE 实时事件流", "operationId": "stream", "tags": []string{"admin"},
				"security":  []map[string]any{{"AdminToken": []string{}}},
				"responses": map[string]any{"200": map[string]any{"description": "text/event-stream"}},
			},
		},
	},
	"components": map[string]any{
		"schemas": map[string]any{
			"RuleInput": map[string]any{
				"type":     "object",
				"required": []string{"name", "source", "op", "schedule", "exec_argv"},
				"properties": map[string]any{
					"name":         map[string]any{"type": "string", "description": "规则名", "example": "main 分支推送"},
					"source":       map[string]any{"type": "string", "enum": []string{"body", "query", "header"}, "description": "匹配来源"},
					"path":         map[string]any{"type": "string", "description": "body 用点路径（如 action、user.login），query/header 用键名"},
					"op":           map[string]any{"type": "string", "enum": []string{"eq", "contains", "prefix", "regex", "exists"}, "description": "比较方式"},
					"value":        map[string]any{"type": "string", "description": "比较值（op=exists 时可空）"},
					"job_name":     map[string]any{"type": "string", "description": "生成的任务名，留空用规则名"},
					"schedule":     map[string]any{"type": "string", "example": "@every 5m", "description": "@every 30s / @every 5m / @every 2h，或 5 段 cron（如 0 3 * * *）"},
					"exec_argv":    map[string]any{"type": "string", "description": "JSON 字符串数组，支持 {{ payload.x }} 模板", "example": "[\"/bin/sh\",\"-c\",\"echo {{ payload.repo }}\"]"},
					"timeout_sec":  map[string]any{"type": "integer", "default": 60, "description": "执行超时秒数，超时杀整个进程组"},
					"max_attempts": map[string]any{"type": "integer", "default": 3, "description": "最大尝试次数，用尽后进死信"},
					"mode": map[string]any{
						"type": "string", "enum": []string{"recurring", "per_event"}, "default": "recurring",
						"description": "recurring：同一规则只维护一条常驻任务，命中只刷新 payload 与下次执行时间；per_event：每次命中都新建一条一次性任务并立即执行。",
					},
					"enabled": map[string]any{"type": "boolean", "default": true},
				},
			},
			"JobInput": map[string]any{
				"type":     "object",
				"required": []string{"name", "schedule", "exec_argv"},
				"properties": map[string]any{
					"name":         map[string]any{"type": "string", "description": "任务名"},
					"schedule":     map[string]any{"type": "string", "example": "@every 30s"},
					"exec_argv":    map[string]any{"type": "string", "description": "JSON 字符串数组"},
					"payload":      map[string]any{"type": "string", "description": "JSON 字符串，供 {{ payload.x }} 使用"},
					"timeout_sec":  map[string]any{"type": "integer", "default": 60},
					"max_attempts": map[string]any{"type": "integer", "default": 3},
					"enabled":      map[string]any{"type": "boolean", "default": true},
					"notify": map[string]any{
						"type": "array", "description": "任务级推送绑定：选渠道 + 触发时机",
						"items": map[string]any{"type": "object", "required": []string{"channel_id"},
							"properties": map[string]any{
								"channel_id": map[string]any{"type": "integer", "description": "渠道 id"},
								"events": map[string]any{"type": "array", "items": map[string]any{"type": "string", "enum": []string{"success", "failure"}},
									"description": "成功/失败时推送；空数组=两者都推"},
							}},
					},
				},
			},
			"ChannelInput": map[string]any{
				"type":     "object",
				"required": []string{"name", "type", "config"},
				"properties": map[string]any{
					"name": map[string]any{"type": "string", "description": "渠道名，如「我的 iPhone」"},
					"type": map[string]any{"type": "string",
						"enum":        []string{"bark", "wecom", "dingtalk", "feishu", "serverchan", "pushplus", "telegram", "slack", "discord", "ntfy", "gotify", "email", "webhook"},
						"description": "渠道类型，字段定义见 GET /api/channels/types"},
					"config":  map[string]any{"type": "object", "additionalProperties": map[string]any{"type": "string"}, "description": "类型相关配置"},
					"enabled": map[string]any{"type": "boolean", "default": true},
				},
			},
		},
		"securitySchemes": map[string]any{
			"ApiToken": map[string]any{
				"type": "apiKey", "in": "header", "name": "X-Api-Token",
				"description": "访问令牌。在「设置 → 令牌」里创建，按访问对象分配不同权限（hook.send / hook.trigger / job.read 等）。服务端建过令牌后，webhook 与管理接口都必须携带。",
			},
			"AdminToken": map[string]any{
				"type": "apiKey", "in": "header", "name": "X-Admin-Token",
				"description": "环境变量 ADMIN_TOKEN（全权，兼容旧部署）。日常请改用 X-Api-Token 创建的多令牌。",
			},
			"HookToken": map[string]any{
				"type": "apiKey", "in": "header", "name": "X-Hook-Token",
				"description": "端点 auth=token 时使用。",
			},
			"HmacSig": map[string]any{
				"type": "apiKey", "in": "header", "name": "X-Hub-Signature-256",
				"description": "端点 auth=hmac 时使用，格式 sha256=<hex(HMAC-SHA256(body, secret))>。",
			},
		},
	},
	"tags": []map[string]any{
		{"name": "webhook", "description": "对外接收接口"},
		{"name": "admin", "description": "管理接口"},
		{"name": "tokens", "description": "令牌与权限"},
		{"name": "channels", "description": "推送渠道"},
		{"name": "ops", "description": "运维"},
	},
}

func openapiHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(openapiDoc)
}

// 自动生成：勿手改。来源 scripts/gen-openapi-file.sh，内容为 /openapi.json 原文。
export const OPENAPI = `{
  "components": {
    "schemas": {
      "ChannelInput": {
        "properties": {
          "config": {
            "additionalProperties": {
              "type": "string"
            },
            "description": "类型相关配置",
            "type": "object"
          },
          "enabled": {
            "default": true,
            "type": "boolean"
          },
          "name": {
            "description": "渠道名，如「我的 iPhone」",
            "type": "string"
          },
          "type": {
            "description": "渠道类型，字段定义见 GET /api/channels/types",
            "enum": [
              "bark",
              "wecom",
              "dingtalk",
              "feishu",
              "serverchan",
              "pushplus",
              "telegram",
              "slack",
              "discord",
              "ntfy",
              "gotify",
              "email",
              "webhook"
            ],
            "type": "string"
          }
        },
        "required": [
          "name",
          "type",
          "config"
        ],
        "type": "object"
      },
      "JobInput": {
        "properties": {
          "enabled": {
            "default": true,
            "type": "boolean"
          },
          "exec_argv": {
            "description": "JSON 字符串数组",
            "type": "string"
          },
          "max_attempts": {
            "default": 3,
            "type": "integer"
          },
          "name": {
            "description": "任务名",
            "type": "string"
          },
          "notify": {
            "description": "任务级推送绑定：选渠道 + 触发时机",
            "items": {
              "properties": {
                "channel_id": {
                  "description": "渠道 id",
                  "type": "integer"
                },
                "events": {
                  "description": "成功/失败时推送；空数组=两者都推",
                  "items": {
                    "enum": [
                      "success",
                      "failure"
                    ],
                    "type": "string"
                  },
                  "type": "array"
                }
              },
              "required": [
                "channel_id"
              ],
              "type": "object"
            },
            "type": "array"
          },
          "payload": {
            "description": "JSON 字符串，供 {{ payload.x }} 使用",
            "type": "string"
          },
          "schedule": {
            "example": "@every 30s",
            "type": "string"
          },
          "timeout_sec": {
            "default": 60,
            "type": "integer"
          }
        },
        "required": [
          "name",
          "schedule",
          "exec_argv"
        ],
        "type": "object"
      },
      "RuleInput": {
        "properties": {
          "enabled": {
            "default": true,
            "type": "boolean"
          },
          "exec_argv": {
            "description": "JSON 字符串数组，支持 {{ payload.x }} 模板",
            "example": "[\\"/bin/sh\\",\\"-c\\",\\"echo {{ payload.repo }}\\"]",
            "type": "string"
          },
          "job_name": {
            "description": "生成的任务名，留空用规则名",
            "type": "string"
          },
          "max_attempts": {
            "default": 3,
            "description": "最大尝试次数，用尽后进死信",
            "type": "integer"
          },
          "mode": {
            "default": "recurring",
            "description": "recurring：同一规则只维护一条常驻任务，命中只刷新 payload 与下次执行时间；per_event：每次命中都新建一条一次性任务并立即执行。",
            "enum": [
              "recurring",
              "per_event"
            ],
            "type": "string"
          },
          "name": {
            "description": "规则名",
            "example": "main 分支推送",
            "type": "string"
          },
          "op": {
            "description": "比较方式",
            "enum": [
              "eq",
              "contains",
              "prefix",
              "regex",
              "exists"
            ],
            "type": "string"
          },
          "path": {
            "description": "body 用点路径（如 action、user.login），query/header 用键名",
            "type": "string"
          },
          "schedule": {
            "description": "@every 30s / @every 5m / @every 2h，或 5 段 cron（如 0 3 * * *）",
            "example": "@every 5m",
            "type": "string"
          },
          "source": {
            "description": "匹配来源",
            "enum": [
              "body",
              "query",
              "header"
            ],
            "type": "string"
          },
          "timeout_sec": {
            "default": 60,
            "description": "执行超时秒数，超时杀整个进程组",
            "type": "integer"
          },
          "value": {
            "description": "比较值（op=exists 时可空）",
            "type": "string"
          }
        },
        "required": [
          "name",
          "source",
          "op",
          "schedule",
          "exec_argv"
        ],
        "type": "object"
      }
    },
    "securitySchemes": {
      "AdminToken": {
        "description": "环境变量 ADMIN_TOKEN（全权，兼容旧部署）。日常请改用 X-Api-Token 创建的多令牌。",
        "in": "header",
        "name": "X-Admin-Token",
        "type": "apiKey"
      },
      "ApiToken": {
        "description": "访问令牌。在「设置 → 令牌」里创建，按访问对象分配不同权限（hook.send / hook.trigger / job.read 等）。服务端建过令牌后，webhook 与管理接口都必须携带。",
        "in": "header",
        "name": "X-Api-Token",
        "type": "apiKey"
      },
      "HmacSig": {
        "description": "端点 auth=hmac 时使用，格式 sha256=<hex(HMAC-SHA256(body, secret))>。",
        "in": "header",
        "name": "X-Hub-Signature-256",
        "type": "apiKey"
      },
      "HookToken": {
        "description": "端点 auth=token 时使用。",
        "in": "header",
        "name": "X-Hook-Token",
        "type": "apiKey"
      }
    }
  },
  "info": {
    "description": "Webhook 接收 + 规则触发定时任务的网关。POST 任意 body 到 /hook/{slug} 即可留档，命中规则时自动生成可重试的定时任务；任务成功或重试用尽后可推送到 Bark、企业微信、钉钉、飞书、Telegram、Slack、Discord 等渠道。",
    "title": "hookdock API",
    "version": "0.2.0"
  },
  "openapi": "3.1.0",
  "paths": {
    "/api/channels": {
      "get": {
        "operationId": "listChannels",
        "responses": {
          "200": {
            "description": "渠道数组。无 channel.manage 权限时 config 字段为空。"
          }
        },
        "security": [
          {
            "ApiToken": []
          }
        ],
        "summary": "推送渠道列表",
        "tags": [
          "channels"
        ]
      },
      "post": {
        "operationId": "createChannel",
        "requestBody": {
          "content": {
            "application/json": {
              "schema": {
                "$ref": "#/components/schemas/ChannelInput"
              }
            }
          },
          "required": true
        },
        "responses": {
          "200": {
            "description": "已创建的渠道"
          },
          "400": {
            "description": "类型未知或必填配置缺失"
          }
        },
        "security": [
          {
            "ApiToken": []
          }
        ],
        "summary": "新建推送渠道",
        "tags": [
          "channels"
        ]
      }
    },
    "/api/channels/types": {
      "get": {
        "operationId": "channelTypes",
        "responses": {
          "200": {
            "description": "渠道类型与各类型配置字段"
          }
        },
        "security": [
          {
            "ApiToken": []
          }
        ],
        "summary": "推送渠道类型目录",
        "tags": [
          "channels"
        ]
      }
    },
    "/api/channels/{id}": {
      "delete": {
        "operationId": "deleteChannel",
        "responses": {
          "200": {
            "description": "删除成功"
          }
        },
        "security": [
          {
            "ApiToken": []
          }
        ],
        "summary": "删除推送渠道",
        "tags": [
          "channels"
        ]
      },
      "get": {
        "operationId": "getChannel",
        "responses": {
          "200": {
            "description": "渠道详情"
          }
        },
        "security": [
          {
            "ApiToken": []
          }
        ],
        "summary": "推送渠道详情",
        "tags": [
          "channels"
        ]
      },
      "put": {
        "operationId": "updateChannel",
        "responses": {
          "200": {
            "description": "更新后的渠道"
          }
        },
        "security": [
          {
            "ApiToken": []
          }
        ],
        "summary": "修改推送渠道",
        "tags": [
          "channels"
        ]
      }
    },
    "/api/channels/{id}/test": {
      "post": {
        "operationId": "testChannel",
        "responses": {
          "200": {
            "description": "渠道接受消息"
          },
          "502": {
            "description": "渠道返回错误，error 字段是原始错误（不含密钥）"
          }
        },
        "security": [
          {
            "ApiToken": []
          }
        ],
        "summary": "发送测试消息",
        "tags": [
          "channels"
        ]
      }
    },
    "/api/endpoints": {
      "get": {
        "operationId": "listEndpoints",
        "responses": {
          "200": {
            "description": "端点数组"
          }
        },
        "security": [
          {
            "AdminToken": []
          }
        ],
        "summary": "端点列表",
        "tags": [
          "admin"
        ]
      },
      "post": {
        "operationId": "createEndpoint",
        "responses": {
          "200": {
            "description": "已创建的端点"
          }
        },
        "security": [
          {
            "AdminToken": []
          }
        ],
        "summary": "新建端点",
        "tags": [
          "admin"
        ]
      }
    },
    "/api/endpoints/{key}/requests": {
      "get": {
        "operationId": "listEndpointRequests",
        "parameters": [
          {
            "description": "slug 或 id",
            "in": "path",
            "name": "key",
            "required": true,
            "schema": {
              "type": "string"
            }
          },
          {
            "in": "query",
            "name": "limit",
            "schema": {
              "default": 50,
              "type": "integer"
            }
          },
          {
            "description": "游标：只看该时间戳之前",
            "in": "query",
            "name": "before",
            "schema": {
              "type": "integer"
            }
          }
        ],
        "responses": {
          "200": {
            "description": "请求数组"
          }
        },
        "security": [
          {
            "AdminToken": []
          }
        ],
        "summary": "端点收到的请求",
        "tags": [
          "admin"
        ]
      }
    },
    "/api/endpoints/{key}/rules": {
      "get": {
        "operationId": "listRules",
        "responses": {
          "200": {
            "description": "规则数组"
          }
        },
        "security": [
          {
            "AdminToken": []
          }
        ],
        "summary": "端点规则列表",
        "tags": [
          "admin"
        ]
      },
      "post": {
        "operationId": "createRule",
        "requestBody": {
          "content": {
            "application/json": {
              "schema": {
                "$ref": "#/components/schemas/RuleInput"
              }
            }
          },
          "required": true
        },
        "responses": {
          "200": {
            "description": "已创建的规则"
          }
        },
        "security": [
          {
            "AdminToken": []
          }
        ],
        "summary": "新增规则",
        "tags": [
          "admin"
        ]
      }
    },
    "/api/jobs": {
      "get": {
        "operationId": "listJobs",
        "responses": {
          "200": {
            "description": "任务数组"
          }
        },
        "security": [
          {
            "AdminToken": []
          }
        ],
        "summary": "任务列表",
        "tags": [
          "admin"
        ]
      },
      "post": {
        "operationId": "createJob",
        "requestBody": {
          "content": {
            "application/json": {
              "schema": {
                "$ref": "#/components/schemas/JobInput"
              }
            }
          },
          "required": true
        },
        "responses": {
          "200": {
            "description": "已创建的任务"
          }
        },
        "security": [
          {
            "AdminToken": []
          }
        ],
        "summary": "新建任务",
        "tags": [
          "admin"
        ]
      }
    },
    "/api/jobs/{id}/run": {
      "post": {
        "operationId": "runJob",
        "responses": {
          "200": {
            "description": "已入队"
          }
        },
        "security": [
          {
            "AdminToken": []
          }
        ],
        "summary": "立即执行一次",
        "tags": [
          "admin"
        ]
      }
    },
    "/api/jobs/{id}/toggle": {
      "post": {
        "operationId": "toggleJob",
        "responses": {
          "200": {
            "description": "当前启用状态"
          }
        },
        "security": [
          {
            "AdminToken": []
          }
        ],
        "summary": "启用 / 暂停",
        "tags": [
          "admin"
        ]
      }
    },
    "/api/overview": {
      "get": {
        "operationId": "overview",
        "responses": {
          "200": {
            "description": "统计 + 端点 + 最近请求/运行"
          }
        },
        "security": [
          {
            "AdminToken": []
          }
        ],
        "summary": "概览统计",
        "tags": [
          "admin"
        ]
      }
    },
    "/api/requests/{id}": {
      "get": {
        "operationId": "getRequest",
        "responses": {
          "200": {
            "description": "完整请求（body/headers/payload）"
          }
        },
        "security": [
          {
            "AdminToken": []
          }
        ],
        "summary": "请求详情",
        "tags": [
          "admin"
        ]
      }
    },
    "/api/requests/{id}/make-job": {
      "post": {
        "operationId": "requestToJob",
        "requestBody": {
          "content": {
            "application/json": {
              "schema": {
                "$ref": "#/components/schemas/JobInput"
              }
            }
          },
          "required": true
        },
        "responses": {
          "200": {
            "description": "已创建的任务"
          }
        },
        "security": [
          {
            "AdminToken": []
          }
        ],
        "summary": "把一条请求转成定时任务",
        "tags": [
          "admin"
        ]
      }
    },
    "/api/runs": {
      "get": {
        "operationId": "listRuns",
        "parameters": [
          {
            "in": "query",
            "name": "job",
            "schema": {
              "type": "string"
            }
          },
          {
            "in": "query",
            "name": "status",
            "schema": {
              "type": "string"
            }
          },
          {
            "in": "query",
            "name": "limit",
            "schema": {
              "default": 50,
              "type": "integer"
            }
          }
        ],
        "responses": {
          "200": {
            "description": "运行数组"
          }
        },
        "security": [
          {
            "AdminToken": []
          }
        ],
        "summary": "运行记录",
        "tags": [
          "admin"
        ]
      }
    },
    "/api/runs/{id}/retry": {
      "post": {
        "operationId": "retryRun",
        "responses": {
          "200": {
            "description": "已重新入队"
          }
        },
        "security": [
          {
            "AdminToken": []
          }
        ],
        "summary": "死信重投",
        "tags": [
          "admin"
        ]
      }
    },
    "/api/stream": {
      "get": {
        "operationId": "stream",
        "responses": {
          "200": {
            "description": "text/event-stream"
          }
        },
        "security": [
          {
            "AdminToken": []
          }
        ],
        "summary": "SSE 实时事件流",
        "tags": [
          "admin"
        ]
      }
    },
    "/api/tokens": {
      "get": {
        "responses": {
          "200": {
            "description": "令牌列表（不含明文与 hash）"
          }
        },
        "security": [
          {
            "ApiToken": []
          }
        ],
        "summary": "列出令牌",
        "tags": [
          "tokens"
        ]
      },
      "post": {
        "requestBody": {
          "content": {
            "application/json": {
              "schema": {
                "properties": {
                  "name": {
                    "description": "访问对象，如「CI 机器人」「张三-只读」",
                    "type": "string"
                  },
                  "note": {
                    "type": "string"
                  },
                  "perms": {
                    "description": "细粒度权限，见 GET /api/whoami 的 all_perms",
                    "items": {
                      "type": "string"
                    },
                    "type": "array"
                  },
                  "role": {
                    "description": "角色预设；与 perms 同时给出时 perms 优先",
                    "enum": [
                      "sender",
                      "trigger",
                      "operator",
                      "admin"
                    ],
                    "type": "string"
                  },
                  "scopes": {
                    "description": "限定可访问的端点 slug；空数组=全部",
                    "items": {
                      "type": "string"
                    },
                    "type": "array"
                  }
                },
                "required": [
                  "name"
                ],
                "type": "object"
              }
            }
          },
          "required": true
        },
        "responses": {
          "200": {
            "description": "创建成功，响应里的 plain 字段是明文令牌，只显示这一次"
          }
        },
        "security": [
          {
            "ApiToken": []
          }
        ],
        "summary": "创建令牌",
        "tags": [
          "tokens"
        ]
      }
    },
    "/api/tokens/{id}": {
      "delete": {
        "responses": {
          "200": {
            "description": "删除成功"
          }
        },
        "security": [
          {
            "ApiToken": []
          }
        ],
        "summary": "删除令牌",
        "tags": [
          "tokens"
        ]
      },
      "put": {
        "responses": {
          "200": {
            "description": "更新后的令牌"
          }
        },
        "security": [
          {
            "ApiToken": []
          }
        ],
        "summary": "修改令牌（名称/角色/权限/作用域/启停）",
        "tags": [
          "tokens"
        ]
      }
    },
    "/api/whoami": {
      "get": {
        "responses": {
          "200": {
            "description": "身份、权限清单、角色预设"
          },
          "401": {
            "description": "未携带令牌"
          }
        },
        "summary": "当前令牌身份与权限",
        "tags": [
          "tokens"
        ]
      }
    },
    "/healthz": {
      "get": {
        "operationId": "healthz",
        "responses": {
          "200": {
            "description": "ok"
          }
        },
        "summary": "健康检查",
        "tags": [
          "ops"
        ]
      }
    },
    "/hook/{slug}": {
      "get": {
        "operationId": "receiveHookGet",
        "parameters": [
          {
            "in": "path",
            "name": "slug",
            "required": true,
            "schema": {
              "type": "string"
            }
          }
        ],
        "responses": {
          "200": {
            "description": "已接收"
          }
        },
        "summary": "接收 webhook（GET 形式，常用于探活与简单触发）",
        "tags": [
          "webhook"
        ]
      },
      "post": {
        "operationId": "receiveHook",
        "parameters": [
          {
            "in": "path",
            "name": "slug",
            "required": true,
            "schema": {
              "type": "string"
            }
          },
          {
            "description": "服务端建过令牌后必填。令牌需具备 hook.send 权限，且作用域覆盖该端点。",
            "in": "header",
            "name": "X-Api-Token",
            "required": false,
            "schema": {
              "type": "string"
            }
          },
          {
            "description": "可选。一行 JSON 任务指令，让本条请求顺带创建任务；需要令牌具备 hook.trigger 权限。字段同 body.__job__。",
            "in": "header",
            "name": "X-Hook-Job",
            "required": false,
            "schema": {
              "type": "string"
            }
          }
        ],
        "requestBody": {
          "content": {
            "application/json": {
              "schema": {
                "properties": {
                  "__job__": {
                    "description": "可选。附带任务指令，替代 X-Hook-Job 头。需要 hook.trigger 权限。",
                    "properties": {
                      "exec_argv": {
                        "items": {
                          "type": "string"
                        },
                        "type": "array"
                      },
                      "max_attempts": {
                        "maximum": 10,
                        "minimum": 1,
                        "type": "integer"
                      },
                      "name": {
                        "type": "string"
                      },
                      "oneshot": {
                        "description": "true=只执行一次后自动停用",
                        "type": "boolean"
                      },
                      "payload": {
                        "description": "覆盖给任务的 payload，默认用本次请求的 payload",
                        "type": "string"
                      },
                      "schedule": {
                        "description": "5 段 cron 或 @every 30s；默认 @every 5m",
                        "type": "string"
                      },
                      "timeout_sec": {
                        "maximum": 3600,
                        "minimum": 1,
                        "type": "integer"
                      }
                    },
                    "required": [
                      "exec_argv"
                    ],
                    "type": "object"
                  }
                },
                "type": "object"
              }
            },
            "application/x-www-form-urlencoded": {
              "schema": {
                "type": "object"
              }
            },
            "text/plain": {
              "schema": {
                "type": "string"
              }
            }
          },
          "required": false
        },
        "responses": {
          "200": {
            "content": {
              "application/json": {
                "schema": {
                  "properties": {
                    "endpoint": {
                      "type": "string"
                    },
                    "inline_jobs": {
                      "description": "仅 X-Hook-Job / __job__ 指令创建的",
                      "items": {
                        "type": "string"
                      },
                      "type": "array"
                    },
                    "matched_jobs": {
                      "description": "规则命中 + 内联指令创建的全部任务 id",
                      "items": {
                        "type": "string"
                      },
                      "type": "array"
                    },
                    "ok": {
                      "type": "boolean"
                    },
                    "received_at": {
                      "type": "integer"
                    },
                    "request_id": {
                      "type": "string"
                    },
                    "rule_jobs": {
                      "description": "仅规则命中创建的",
                      "items": {
                        "type": "string"
                      },
                      "type": "array"
                    }
                  },
                  "type": "object"
                }
              }
            },
            "description": "已接收并留档"
          },
          "401": {
            "description": "鉴权失败（令牌缺失/无效，或端点 token / HMAC 校验失败）"
          },
          "403": {
            "description": "令牌缺少 hook.send / hook.trigger 权限，或作用域不含该端点"
          },
          "404": {
            "description": "端点不存在或已停用"
          },
          "429": {
            "description": "超过速率限制"
          }
        },
        "summary": "接收 webhook",
        "tags": [
          "webhook"
        ]
      }
    }
  },
  "servers": [
    {
      "description": "当前实例",
      "url": "/"
    }
  ],
  "tags": [
    {
      "description": "对外接收接口",
      "name": "webhook"
    },
    {
      "description": "管理接口",
      "name": "admin"
    },
    {
      "description": "令牌与权限",
      "name": "tokens"
    },
    {
      "description": "推送渠道",
      "name": "channels"
    },
    {
      "description": "运维",
      "name": "ops"
    }
  ]
}`

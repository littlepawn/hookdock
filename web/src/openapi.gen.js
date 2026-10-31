// 自动生成：勿手改。来源 scripts/gen-openapi-file.sh，内容为 /openapi.json 原文。
export const OPENAPI = `{
  "components": {
    "schemas": {
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
        "description": "管理接口令牌，取 HOOKDOCK_ADMIN_TOKEN；未设置时管理接口开放（仅限本机使用）。",
        "in": "header",
        "name": "X-Admin-Token",
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
    "description": "Webhook 接收 + 规则触发定时任务的网关。POST 任意 body 到 /hook/{slug} 即可留档，命中规则时自动生成可重试的定时任务。",
    "title": "hookdock API",
    "version": "0.1.0"
  },
  "openapi": "3.1.0",
  "paths": {
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
          }
        ],
        "requestBody": {
          "content": {
            "application/json": {
              "schema": {
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
                    "matched_jobs": {
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
                    }
                  },
                  "type": "object"
                }
              }
            },
            "description": "已接收并留档"
          },
          "401": {
            "description": "鉴权失败（token / HMAC）"
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
      "description": "运维",
      "name": "ops"
    }
  ]
}`

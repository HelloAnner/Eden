# 博客第三方 REST 接口设计（文件夹驱动 + SQLite 同步）

## 基础信息

- Base URL: `http://<host>:20260/api/v1`
- 数据格式: `application/json`
- 编码: `UTF-8`
- 文章来源: `data/notes/{标签树}/{标题}.md`
- 同步原则: 以文件夹和 Markdown 文件为唯一事实来源
- ID 规则: 后端按 `path + title` 自动生成稳定 `id`，并写入 `note_identity_map`
- 标签层级: 按目录自动同步到 `note_tag_hierarchy`

## 1. 获取文章树（含文件夹 + 文章）

- `GET /articles/tree`

## 2. 获取纯标签结构树（不含文章）

- `GET /tags/tree`

说明：

- 返回当前 SQLite 聚合后的标签层级树
- 仅包含标签节点，不包含文章节点
- 数据来源：`note_tag_hierarchy`

## 3. 获取最近更新文章列表（首页）

- `GET /articles/recent?limit=20`

说明：

- 按 `updated_at` 倒序
- `limit` 默认 `20`，最大 `200`
- 返回 `created_at`、`updated_at`

## 4. 获取文章详情

- `GET /articles/{id}`

返回：

- `article`: 文章详情
- `comments`: 评论树（含回复）

## 5. 搜索文章

- `GET /articles/search?q=<keyword>`

返回 `total/items`，支持标题、摘要、路径、标签文本匹配。

## 6. 辅助接口（页面内容）

- `GET /config`: 获取站点配置（`config.toml` 映射）
- `PUT /config`: 更新站点配置
- `GET /data/{path}`: 获取 `data` 静态资源（头像、站点图标、附件）
- `GET /comments?article_id={id}`: 获取评论树
- `POST /comments`: 新增评论/回复（`parent_id` 可选）
- `POST /comments/{id}/like`: 点赞评论
- `POST /subscribe`: 订阅邮箱
- `GET /profile/stats`: 个人统计

## 7. 扫描与同步机制

- 扫描周期: 默认 60 秒
- 扫描周期配置: `config.toml` -> `[system] scan_interval_seconds`
- 扫描范围: `data/notes/**/*.md`（跳过 `attachments/comments` 目录）
- 任意文件夹变更（新增/修改/删除笔记）都会在扫描后同步到 SQLite
- 同步到 SQLite:
  - `note_metadata`: 文章基础元数据
  - `note_identity_map`: `path+title -> id` 映射
  - `note_tag_hierarchy`: 标签目录树层级
  - `note_visual_metadata`: 左侧节点图标
  - `note_runtime_metadata`: 阅读次数等运行时数据
  - `comments` / `comment_like_events`: 评论与点赞数据

## 8. 订阅推送机制

- 接口: `POST /subscribe`
- 订阅存储: `subscribers` 表（`active/updated_at/last_sent_at/last_error`）
- 推送触发: 扫描发现新增文章后异步入队
- 发送策略: 单 worker 队列发送，每个邮箱间隔 `3s`
- 邮件内容: 使用 Obsidian 兼容渲染后输出 HTML，尽量与前端阅读效果一致
- 邮件配置: `config.toml` -> `[mail]`

## 9. 已移除接口

以下接口已移除，不再对外提供（返回 404）：

- `POST /articles`
- `PUT /articles/{id}`
- `PATCH /articles/{id}/move`
- `DELETE /articles/{id}`

说明：

- 后续新增/修改/删除文章请直接在 `data/notes` 文件夹操作文件。

## 10. 日志系统

- 日志目录: `logs/`（容器内 `/app/logs`）
- 日志级别配置: `config.toml` -> `[logging] level`，默认 `INFO`
- 格式: `yyyy-MM-dd HH:mm:ss.SSS [LEVEL] [component] message`
- 按天切分: `blog-YYYY-MM-DD.log`
- 自动压缩: 历史 `.log` 自动压缩为 `.log.gz`
- 关键异常（如邮件发送失败）会以 `ERROR` 级别写入

## 11. 评论防爆破

- 限流键: `IP + action`
- 动作:
  - `comment`: `POST /comments`
  - `like`: `POST /comments/{id}/like`
- 默认窗口: 60 秒
- 默认阈值:
  - `comment`: 15 次/分钟/IP
  - `like`: 60 次/分钟/IP
- 超限: `429 Too Many Requests`

## 12. 错误码约定

- `400 Bad Request`: 参数错误/字段缺失
- `404 Not Found`: 资源不存在
- `429 Too Many Requests`: 命中限流
- `500 Internal Server Error`: 服务端异常

统一格式：

```json
{
  "error": "error message"
}
```

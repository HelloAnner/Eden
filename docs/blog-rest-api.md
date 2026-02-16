# 博客第三方 REST 接口设计（Markdown 源 + SQLite 聚合）

## 基础信息

- Base URL: `http://<host>:20260/api/v1`
- 数据格式: `application/json`
- 编码: `UTF-8`
- 文章内容源: `data/notes/{标签树}/{标题}.md`
- 全局唯一键: `path + title`（例如 `Agent工程/MCP + MCP 协议`）
- 标识映射表: `note_identity_map`（`path + title -> id`）
- 标签层级表: `note_tag_hierarchy`（按目录层级自动同步）
- 标签约定: Markdown 底部可写 `#标签`，目录树以文件夹层级为准
- 聚合索引: 后端每 60 秒扫描一次 Markdown 并同步 SQLite

## 1. 获取左侧标签树

- `GET /articles/tree`

## 1.1 获取最近更新文章列表（首页）

- `GET /articles/recent?limit=20`

说明：

- 按 `updated_at` 倒序
- `limit` 默认 `20`，最大 `200`
- 返回 `created_at`、`updated_at`

## 2. 获取文章详情

- `GET /articles/{id}`

返回：

- `article`: 文章详情
- `comments`: 评论树（含回复）

## 3. 新增文章（上传）

- `POST /articles`

请求体（`id` 可选）：

```json
{
  "id": "optional-custom-id",
  "title": "文章标题",
  "category": "Agent工程",
  "path": "Agent工程/MCP",
  "excerpt": "摘要",
  "content": "正文内容（Markdown）"
}
```

行为：

- 持久化为 `data/notes/Agent工程/MCP/文章标题.md`
- 若同路径同标题已存在，返回 `400`
- 若不传 `id`，后端按 `path+title` 生成稳定 `id`

成功响应：`201 Created`，直接返回创建后的完整文章对象（含最终 `id`）

## 4. 更新文章（编辑）

- `PUT /articles/{id}`

请求体与新增一致（不含 `id` 也可，URL 中 `id` 为准）

行为：

- 支持改标题、改路径（即文件重命名/移动）
- 更新后自动同步 `note_identity_map`，保证 `id` 稳定

成功响应：

```json
{
  "id": "article-id",
  "status": "updated"
}
```

## 5. 移动文章（调整树结构）

- `PATCH /articles/{id}/move`

请求体：

```json
{
  "parent_id": "Agent工程/记忆系统/RAG",
  "order_index": 3
}
```

说明：

- `parent_id` 表示目标目录路径（非文章 id）
- 会触发文件移动并重建目录树

## 6. 删除文章

- `DELETE /articles/{id}`

行为：

- 删除对应 Markdown 文件
- 自动清理空目录
- 自动清理 SQLite 中的文章、评论关联与标签层级

成功响应：`204 No Content`

## 7. 搜索文章

- `GET /articles/search?q=<keyword>`

返回 `total/items`，支持标题、摘要、路径、标签文本匹配。

## 8. 辅助接口（页面内容）

- `GET /config`: 获取站点配置（`config.toml` 映射）
- `PUT /config`: 更新站点配置
- `GET /data/{path}`: 获取 `data` 静态资源（头像、站点图标、附件）
- `GET /comments?article_id={id}`: 获取评论树
- `POST /comments`: 新增评论/回复（`parent_id` 可选）
- `POST /comments/{id}/like`: 点赞评论
- `POST /subscribe`: 订阅邮箱
- `GET /profile/stats`: 个人统计

## 9. 扫描与同步机制

- 扫描周期: 60 秒
- 扫描范围: `data/notes/**/*.md`（跳过 `attachments/comments` 目录）
- 同步到 SQLite:
  - `note_metadata`: 文章基础元数据
  - `note_identity_map`: `path+title -> id` 映射
  - `note_tag_hierarchy`: 标签目录树层级
  - `note_visual_metadata`: 左侧节点图标
  - `note_runtime_metadata`: 阅读次数等运行时数据
  - `comments` / `comment_like_events`: 评论与点赞数据

## 10. 评论防爆破

- 限流键: `IP + action`
- 动作:
  - `comment`: `POST /comments`
  - `like`: `POST /comments/{id}/like`
- 默认窗口: 60 秒
- 默认阈值:
  - `comment`: 15 次/分钟/IP
  - `like`: 60 次/分钟/IP
- 超限: `429 Too Many Requests`

## 11. 错误码约定

- `400 Bad Request`: 参数错误/字段缺失/路径冲突
- `404 Not Found`: 资源不存在
- `429 Too Many Requests`: 命中限流
- `500 Internal Server Error`: 服务端异常

统一格式：

```json
{
  "error": "error message"
}
```

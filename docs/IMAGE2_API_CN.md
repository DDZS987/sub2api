# Image2 图片接口文档

本文档说明如何通过 `https://fyai.space` 调用本项目的 OpenAI 兼容图片接口，覆盖文生图和图片编辑两类能力。

## 基础信息

基础地址：

```text
https://fyai.space
```

鉴权方式：

```http
Authorization: Bearer sk-你的APIKey
```

通用要求：

- API Key 所属分组需要是 OpenAI 平台。
- 分组需要开启图片生成权限，并且有可用的 OpenAI 图片账号。
- 图片模型使用 `gpt-image-2`；不传 `model` 时服务端默认补为 `gpt-image-2`。
- 默认返回 `b64_json`，即图片 Base64 内容。

## 1. 文生图

接口地址：

```http
POST https://fyai.space/v1/images/generations
```

请求头：

```http
Authorization: Bearer sk-你的APIKey
Content-Type: application/json
```

请求示例：

```bash
curl -X POST 'https://fyai.space/v1/images/generations' \
  -H 'Authorization: Bearer sk-你的APIKey' \
  -H 'Content-Type: application/json' \
  -d '{
    "model": "gpt-image-2",
    "prompt": "画一张未来城市夜景，霓虹灯，电影感，高细节",
    "size": "1024x1024",
    "response_format": "b64_json"
  }'
```

常用参数：

| 参数 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `model` | string | 否 | 图片模型，建议 `gpt-image-2` |
| `prompt` | string | 是 | 文生图提示词 |
| `size` | string | 否 | 图片尺寸，如 `1024x1024`、`1536x1024`、`1024x1536`、`2048x1152`、`auto` |
| `response_format` | string | 否 | `b64_json` 或 `url`，默认 `b64_json` |
| `quality` | string | 否 | 图片质量，如 `high`、`medium`、`low`、`auto` |
| `background` | string | 否 | 背景类型，如 `transparent`、`opaque`、`auto` |
| `output_format` | string | 否 | 输出格式，如 `png`、`webp`、`jpeg` |
| `n` | number | 否 | 生成图片数量，默认 `1` |
| `stream` | boolean | 否 | 是否流式返回，默认 `false` |

响应示例：

```json
{
  "created": 1710000000,
  "data": [
    {
      "b64_json": "iVBORw0KGgo...",
      "revised_prompt": "A detailed cinematic neon city at night..."
    }
  ],
  "usage": {
    "input_tokens": 10,
    "output_tokens": 20,
    "output_tokens_details": {
      "image_tokens": 8
    }
  }
}
```

## 2. 图片编辑

接口地址：

```http
POST https://fyai.space/v1/images/edits
```

图片编辑支持两种提交方式：上传本地图片，或在 JSON 中传图片 URL。

### 方式一：上传本地图片

请求头只需要带鉴权，`Content-Type` 由 `curl` 自动生成。

```bash
curl -X POST 'https://fyai.space/v1/images/edits' \
  -H 'Authorization: Bearer sk-你的APIKey' \
  -F 'model=gpt-image-2' \
  -F 'prompt=把图片背景替换成雪山日出，保持主体不变' \
  -F 'image=@/path/to/source.png' \
  -F 'size=1024x1024' \
  -F 'response_format=b64_json'
```

如需使用蒙版，可额外传 `mask`：

```bash
curl -X POST 'https://fyai.space/v1/images/edits' \
  -H 'Authorization: Bearer sk-你的APIKey' \
  -F 'model=gpt-image-2' \
  -F 'prompt=只修改蒙版区域，把它变成蓝色玻璃材质' \
  -F 'image=@/path/to/source.png' \
  -F 'mask=@/path/to/mask.png' \
  -F 'size=1024x1024' \
  -F 'response_format=b64_json'
```

### 方式二：传图片 URL

请求示例：

```bash
curl -X POST 'https://fyai.space/v1/images/edits' \
  -H 'Authorization: Bearer sk-你的APIKey' \
  -H 'Content-Type: application/json' \
  -d '{
    "model": "gpt-image-2",
    "prompt": "参考这张图片，生成同一主体在雨夜街道中的画面",
    "images": [
      {
        "image_url": "https://example.com/source.png"
      }
    ],
    "size": "1024x1024",
    "response_format": "b64_json"
  }'
```

带蒙版的 JSON 示例：

```json
{
  "model": "gpt-image-2",
  "prompt": "只修改蒙版区域，把背景换成森林",
  "images": [
    {
      "image_url": "https://example.com/source.png"
    }
  ],
  "mask": {
    "image_url": "https://example.com/mask.png"
  },
  "size": "1024x1024",
  "response_format": "b64_json"
}
```

图片编辑常用参数：

| 参数 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `model` | string | 否 | 图片模型，建议 `gpt-image-2` |
| `prompt` | string | 是 | 编辑指令 |
| `image` | file | 上传方式必填 | 本地原图文件，字段名为 `image`；也支持多个 `image[]` |
| `images` | array | JSON 方式必填 | 图片 URL 数组，格式为 `[{"image_url":"..."}]` |
| `mask` | file/object | 否 | 蒙版图片；上传方式传文件，JSON 方式传 `{"image_url":"..."}` |
| `size` | string | 否 | 输出尺寸，如 `1024x1024`、`1536x1024`、`1024x1536`、`auto` |
| `response_format` | string | 否 | `b64_json` 或 `url`，默认 `b64_json` |
| `input_fidelity` | string | 否 | 输入图保真度，如 `high` |
| `output_format` | string | 否 | 输出格式，如 `png`、`webp`、`jpeg` |
| `output_compression` | number | 否 | 输出压缩质量，适用于部分格式 |
| `partial_images` | number | 否 | 流式场景下的部分图片数量 |
| `stream` | boolean | 否 | 是否流式返回，默认 `false` |

响应格式与文生图一致：

```json
{
  "created": 1710000000,
  "data": [
    {
      "b64_json": "iVBORw0KGgo...",
      "revised_prompt": "Replace the image background with sunrise mountains..."
    }
  ]
}
```

## 返回图片处理

当 `response_format` 为 `b64_json` 时，取 `data[0].b64_json` 解码即可保存图片。

Python 示例：

```python
import base64
import requests

api_key = "sk-你的APIKey"

resp = requests.post(
    "https://fyai.space/v1/images/generations",
    headers={
        "Authorization": f"Bearer {api_key}",
        "Content-Type": "application/json",
    },
    json={
        "model": "gpt-image-2",
        "prompt": "画一只戴墨镜的橘猫，赛博朋克风格",
        "size": "1024x1024",
        "response_format": "b64_json",
    },
    timeout=180,
)
resp.raise_for_status()

image_b64 = resp.json()["data"][0]["b64_json"]
with open("output.png", "wb") as f:
    f.write(base64.b64decode(image_b64))
```

## 常见错误

| HTTP 状态 | 可能原因 |
| --- | --- |
| `400` | 请求体格式错误、参数类型错误、图片编辑未传图片、模型不是 `gpt-image-*` |
| `401` | API Key 缺失或无效 |
| `402` | 余额或订阅不足 |
| `403` | 分组未开启图片生成权限，或计费校验未通过 |
| `404` | 路径错误，或 API Key 所属分组不是 OpenAI 平台 |
| `413` | 请求体或上传图片过大 |
| `429` | 并发或速率限制 |
| `502` / `503` | 上游账号不可用、上游限流、无可用兼容账号 |

## 补充说明

- 推荐使用完整路径 `https://fyai.space/v1/images/generations` 和 `https://fyai.space/v1/images/edits`。
- 项目也保留了不带 `/v1` 的兼容别名：`/images/generations`、`/images/edits`。
- 如果 `response_format` 设置为 `url`，部分上游路径可能返回 `data:image/png;base64,...` 形式的 Data URL，而不是公网图片链接。

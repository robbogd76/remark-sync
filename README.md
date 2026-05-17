# remark-sync

A Go CLI that pulls documents from a [Remarkable](https://remarkable.com) tablet over USB, OCRs handwritten content with Azure Computer Vision, scans for action items, and posts them to a local task-central REST API. Optionally mirrors OCR'd text as notes to an Obsidian vault.

## How it works

```
Remarkable tablet (USB)
       │
       ▼
  GET /documents/        ← list all documents
       │
       ▼
  GET /download/{id}/pdf ← tablet renders PDF on demand (notebooks and PDFs)
       │
       ▼
  Azure Computer Vision  ← PDF sent directly, no local image conversion
       │
       ▼
  Options
       │
       ├─── POST actions to task-central API   (action items, optional)
       └─── PUT to Obsidian vault      (full OCR text, optional)
```

Incremental sync: the UTC start time of each successful run is saved to `~/.config/remark-sync/state.yaml`. On subsequent runs only documents with a `ModifiedClient` timestamp newer than the last sync are downloaded and processed. Pass `--full` to reprocess everything.

## Prerequisites

| Tool | Purpose | Install (Windows) |
|------|---------|-------------------|
| **Go 1.22+** | Build the tool | `winget install GoLang.Go` |

The tablet must be connected via USB cable with the USB web interface enabled
(Settings → Storage → USB web interface).

### Azure Computer Vision

OCR is performed by [Azure AI Vision](https://learn.microsoft.com/azure/ai-services/computer-vision/).
No local OCR tools are required — the PDF is sent directly to Azure.

1. Create an **Azure AI services** (or **Computer Vision**) resource in the Azure portal.
2. Copy the **endpoint** and one of the **keys** from the *Keys and Endpoint* page.
3. Set them in `~/.config/remark-sync/config.yaml` (see [Configuration reference](#configuration-reference)).

## Installation

```bash
git clone https://github.com/mtlgro/remark-sync
cd remark-sync
go build -o remark-sync.exe .
```

## Quick start

### 1. Connect the tablet

Plug the tablet in via USB and enable the USB web interface:
**Settings → Storage → USB web interface**

Verify connectivity:

```
curl http://10.11.99.1/documents/
```

### 2. Configure

```
remark-sync config init   # writes a default config.yaml
```

Edit `~/.config/remark-sync/config.yaml`:

```yaml
task_api:
  url: http://localhost:3000/api/v1/tasks   # leave empty to disable task posting

ocr:
  azure_endpoint: "https://<resource>.cognitiveservices.azure.com"
  azure_key: "<your-key>"
  lang: ""              # BCP-47 language hint, e.g. "en"; empty = auto-detect

action_patterns:
  - name: ACTION        # label posted to TaskCentral
    start: 'ACTION:'    # regex matched at start of line (case-insensitive)
    end: '\*'           # regex matched at end of line to close the action

# pdf_output_dir: "C:/Users/you/Documents/remarkable-pdfs"  # optional

# obsidian:             # optional Obsidian Local REST API integration
#   url: "http://localhost:27123"
#   api_key: ""
#   base_folder: "reMarkable"
```

### 3. Preview

```
remark-sync sync --dry-run
```

Processes all documents and prints found action items without posting anything or updating the last-sync timestamp.

### 4. Sync

```
remark-sync sync
remark-sync sync --name "Meeting notes"   # only documents matching a substring
remark-sync sync --full                   # reprocess all documents, ignore last-sync time
```

## Commands

| Command | Description |
|---------|-------------|
| `remark-sync list` | List all documents on the tablet |
| `remark-sync sync` | Run the full pipeline (incremental by default) |
| `remark-sync sync --name <text>` | Only process documents whose name contains `<text>` (case-insensitive) |
| `remark-sync sync --dry-run` | Print extracted actions without posting or updating state |
| `remark-sync sync --full` | Process all documents regardless of last-sync timestamp |
| `remark-sync config show` | Print current configuration (secrets masked) |
| `remark-sync config init` | Write a default config file |

## Action item format

Action items are detected using configurable patterns defined in `action_patterns`. Each pattern has:

| Field | Description |
|-------|-------------|
| `name` | Label posted to TaskCentral (e.g. `"ACTION"`) |
| `start` | Regex matched (case-insensitively) at the **start** of a line to open an action. Text on the same line after the match becomes the first part of the description. |
| `end` | *(optional)* Regex matched at the **end** of a line to close the action. When omitted, a blank line closes it. |

Continuation lines (non-blank lines that don't start a new action) are joined with a space into the action's description.

Example using the default pattern (`start: ACTION:`, `end: \*`):

```
ACTION: Follow up with Sarah about the
Q4 budget and deadline *

ACTION: Book the conference room for Thursday *
```

Produces:

```
[ACTION] Follow up with Sarah about the Q4 budget and deadline
[ACTION] Book the conference room for Thursday
```

Multiple patterns can be defined to capture different action types:

```yaml
action_patterns:
  - name: ACTION
    start: 'ACTION:'
    end: '\*'
  - name: MEETING
    start: 'MEETING:'
    end: '\*'
  - name: BUY
    start: 'BUY:'
    end: '\*'
```

## Duplicate detection

Before posting each action item, remark-sync queries TaskCentral for an active task with the same title (case-insensitive). If one already exists the item is skipped and a message is printed. This prevents duplicate tasks when a document is re-synced after minor edits.

## Obsidian integration

When `obsidian.url` is set, the full OCR text of each processed document is written as a Markdown note to an Obsidian vault using the [Local REST API](https://github.com/coddingtonbear/obsidian-local-rest-api) plugin.

Notes are created at `<base_folder>/<reMarkable folder path>/<document name>.md`, mirroring the reMarkable folder structure. Each note includes a YAML front matter block with `source: reMarkable` and a `synced` timestamp.

The default plugin address is `http://localhost:27123`. Set `api_key` to the Bearer token from the plugin settings.

## PDF output

Set `pdf_output_dir` to a local directory to save a copy of each downloaded PDF, mirroring the reMarkable folder structure. After each successful sync, any PDFs in that directory that no longer correspond to a document on the tablet are automatically removed.

## TaskCentral API contract

Actions are POSTed to `POST /api/v1/tasks` (default `http://localhost:3000/api/v1/tasks`).

Field mapping:

| Action field | TaskCentral field | Notes |
|---|---|---|
| Action description | `title` | e.g. `"Follow up with Sarah"` |
| Pattern name | `type` | e.g. `"ACTION"`, `"MEETING"` |
| Source notebook name | `description` | e.g. `"Source: Remarkable — Meeting notes"` |
| *(not sent)* | `status` | Defaults to `pending` |
| *(not sent)* | `priority` | Defaults to `medium` |

Example POST body:

```json
{
  "title": "Follow up with Sarah about the Q4 budget and deadline",
  "type": "ACTION",
  "description": "Source: Remarkable — Meeting 2025-05-14"
}
```

The API returns `201 Created` on success. Validation errors (`400`) are decoded and surfaced as human-readable messages.

## Document support

| Document type | How it's processed |
|---------------|--------------------|
| Native notebook | Tablet renders strokes to PDF on download; PDF sent directly to Azure OCR |
| Uploaded PDF | Downloaded as-is via USB; sent directly to Azure OCR |
| Annotated PDF | Downloaded as PDF (annotations included in the rendered output); sent to Azure OCR |

## Configuration reference

```yaml
# ~/.config/remark-sync/config.yaml

task_api:
  url: "http://localhost:3000/api/v1/tasks"  # leave empty or omit to disable task posting

ocr:
  azure_endpoint: "https://<resource>.cognitiveservices.azure.com"
  azure_key: "<Ocp-Apim-Subscription-Key>"
  lang: ""             # BCP-47 language hint, e.g. "en", "fr"; empty = auto-detect

action_patterns:
  - name: ACTION       # label for TaskCentral type field
    start: 'ACTION:'   # start-of-line regex (case-insensitive); "ACTION:" also matches "ACTION :"
    end: '\*'          # end-of-line regex; omit for blank-line termination

pdf_output_dir: ""     # save downloaded PDFs here (empty = disabled)

obsidian:
  url: ""              # e.g. "http://localhost:27123"; empty = disabled
  api_key: ""          # Bearer token from the Local REST API plugin settings
  base_folder: "reMarkable"  # vault folder; defaults to "reMarkable"
```

State (last-sync timestamp) is stored separately in `~/.config/remark-sync/state.yaml` and is never edited manually.

## Project layout

```
remark-sync/
├── main.go
├── cmd/
│   ├── root.go       command root, config loading
│   ├── list.go       'list' — show tablet documents
│   ├── sync.go       'sync' — main pipeline, PDF save, Obsidian, orphan cleanup
│   └── config.go     'config show / init'
└── internal/
    ├── config/       YAML config (load / save / defaults)
    ├── state/        last-sync timestamp (load / save)
    ├── remarkable/
    │   ├── client.go     USB API: document list + PDF download
    │   └── document.go   Document type (DocumentType / CollectionType)
    ├── ocr/          Azure Computer Vision Read API client
    ├── parser/       configurable start/end regex action extractor
    ├── tasks/        HTTP client for TaskCentral (POST + duplicate check)
    └── obsidian/     Obsidian Local REST API client (optional)
```

## Limitations

- **USB only**: the tablet must be physically connected via USB. Wi-Fi sync is not supported.
- **Azure OCR required**: there is no local OCR fallback; `ocr.azure_endpoint` and `ocr.azure_key` must be set.
- **OCR accuracy**: accuracy depends on Azure Computer Vision. For best results with handwriting, ensure the document has clear, well-spaced lettering.

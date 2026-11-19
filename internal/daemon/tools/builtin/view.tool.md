---
apiVersion: warp/v1alpha1
kind: Tool
metadata:
  name: view
  displayName: View File
  description: Read the contents of a file from the workspace (text with line numbers, or images, PDFs, and audio for multimodal models).
spec:
  annotations:
    isReadOnly: true
    isIdempotent: true
    isDangerous: false
    isOpenWorld: false
    userHint: "Read file: %s"
  inputSchema:
    type: object
    properties:
      path:
        type: string
        description: File path relative to the workspace root or an absolute path inside the workspace.
      offset:
        type: integer
        description: 1-based line number from which to start reading. Ignored for images, PDFs, and audio.
      limit:
        type: integer
        description: Maximum number of lines to return. Ignored for images, PDFs, and audio.
      pages:
        type: string
        description: Optional page range for PDF files (e.g., "1-10" or "3"). Ignored for text, image, and audio files.
    required:
      - path
  outputSchema:
    type: object
    properties:
      path:
        type: string
        description: Normalized workspace-relative file path.
      content:
        type: string
        description: Line-numbered text content of the selected line range, or compact metadata for media files.
      start_line:
        type: integer
        description: 1-based first line number returned.
      end_line:
        type: integer
        description: 1-based last line number returned.
      total_lines:
        type: integer
        description: Total number of lines in the file.
      truncated:
        type: boolean
        description: Whether additional lines remain after end_line.
      hash:
        type: string
        description: SHA-256 hex digest of the full file on disk.
      mime_type:
        type: string
        description: MIME type when the viewed file is supported multimodal media.
      byte_size:
        type: integer
        format: int64
        description: Byte size of the viewed media payload.
      pages:
        type: string
        description: Normalized page range for viewed PDF files.
      modality:
        type: string
        description: Media modality ("image", "pdf", or "audio") when viewing multimodal files.
      omitted:
        type: boolean
        description: True if media content was omitted because the active model lacks support for its modality.
    required:
      - path
      - content
      - start_line
      - end_line
      - total_lines
      - hash
---

Read a workspace file: returns numbered lines for text files (including `.svg`, using optional `offset` and `limit`) or attaches supported media (PNG/JPEG/WebP/GIF, PDF with optional `pages`, WAV/MP3/MP4/OGG/FLAC). Always read an existing file with `view` before `edit`, `write` overwrite, or `remove`.

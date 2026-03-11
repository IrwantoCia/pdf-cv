PDF CV
Generate PDF for CV

## Attribution

This project's resume template is adapted from [jakegut/resume](https://github.com/jakegut/resume) by Jake Gutierrez (MIT).
The original template also credits [sb2nov/resume](https://github.com/sb2nov/resume).

See `THIRD_PARTY_NOTICES.md` for the included license notice.

## Plain Text Import/Export

The CV form supports file-based plain text export/import.

- Use `Export as Text` to download `cv.txt`.
- Edit `cv.txt` in any text editor.
- Use `Import from Text` to load the file back into the form.

### Format Guide

The file uses a versioned format so it can be validated on import.

```text
CV-TEXT-V1
boundary: ==CVBOUNDARY_xxx==

[field]
id: summary
section: Professional Summary
label: summary
instruction: one bullet per line
==CVBOUNDARY_xxx==
- Shipped feature A
- Improved process B
==CVBOUNDARY_xxx==
```

Each field block contains:

- `id`: internal field id (`name`, `phone`, `email`, `linkedin`, `github`, `summary`, `education`, `experience`, `projects`, `skills`)
- `section`: section title shown in the form
- `label`: field label
- `instruction`: one or more lines describing how to fill the field
- Boundary-wrapped content: the actual value imported back into the form

### Editing Rules

- Keep `CV-TEXT-V1` and the `boundary:` line unchanged.
- Keep every `[field]` block and unique `id` exactly once.
- You can freely edit content between boundary lines.
- Preserve the boundary markers around each field value.

If the format is invalid (missing fields, unknown ids, broken boundary, wrong version), import will fail with a status message.

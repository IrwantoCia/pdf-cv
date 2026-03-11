(() => {
  const form = document.getElementById("cv-form");
  const generateBtn = document.getElementById("generate-btn");
  const exportTextBtn = document.getElementById("export-text-btn");
  const importTextBtn = document.getElementById("import-text-btn");
  const importTextFile = document.getElementById("import-text-file");
  const status = document.getElementById("status");

  if (!form || !generateBtn || !exportTextBtn || !importTextBtn || !importTextFile || !status) {
    return;
  }

  const storageKey = "cv.state.v2";
  const textExportVersion = "CV-TEXT-V1";
  let lastStateJSON = "";

  const fieldSchema = [
    { id: "name", kind: "basic", section: "Basics", label: "name", instructions: ["required"] },
    { id: "phone", kind: "basic", section: "Basics", label: "phone", instructions: [] },
    { id: "email", kind: "basic", section: "Basics", label: "email", instructions: [] },
    { id: "linkedin", kind: "basic", section: "Basics", label: "linkedin url", instructions: [] },
    { id: "github", kind: "basic", section: "Basics", label: "github url", instructions: [] },
    {
      id: "summary",
      kind: "textarea",
      section: "Professional Summary",
      label: "summary",
      instructions: ["one bullet per line"],
    },
    {
      id: "education",
      kind: "textarea",
      section: "Education",
      label: "education",
      instructions: ["one entry per line: School || Location || Degree || Date"],
    },
    {
      id: "experience",
      kind: "textarea",
      section: "Work Experience",
      label: "experience",
      instructions: [
        "block format: first line Company || Role || Date || Location || Mode",
        "next lines start with \"- \" for bullets",
        "blank line between blocks",
      ],
    },
    {
      id: "projects",
      kind: "textarea",
      section: "Projects",
      label: "projects",
      instructions: [
        "block format: first line Name || Stack || Date",
        "next lines start with \"- \" for bullets",
        "blank line between blocks",
      ],
    },
    {
      id: "skills",
      kind: "textarea",
      section: "Technical Skills",
      label: "skills",
      instructions: ["one line per group: Group: item1, item2, item3"],
    },
  ];

  const fields = {
    name: document.getElementById("name"),
    phone: document.getElementById("phone"),
    email: document.getElementById("email"),
    linkedin: document.getElementById("linkedin"),
    github: document.getElementById("github"),
    summary: document.getElementById("summary"),
    education: document.getElementById("education"),
    experience: document.getElementById("experience"),
    projects: document.getElementById("projects"),
    skills: document.getElementById("skills"),
  };

  if (Object.values(fields).some((el) => !el)) {
    return;
  }

  const encoder = new TextEncoder();
  const decoder = new TextDecoder();

  const base64UrlEncode = (bytes) => {
    let binary = "";
    for (let i = 0; i < bytes.length; i += 1) {
      binary += String.fromCharCode(bytes[i]);
    }
    return btoa(binary).replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/g, "");
  };

  const base64UrlDecode = (value) => {
    const normalized = value.replace(/-/g, "+").replace(/_/g, "/");
    const padded = normalized + "=".repeat((4 - (normalized.length % 4)) % 4);
    const binary = atob(padded);
    const bytes = new Uint8Array(binary.length);
    for (let i = 0; i < binary.length; i += 1) {
      bytes[i] = binary.charCodeAt(i);
    }
    return bytes;
  };

  const compressBytes = async (bytes) => {
    if (typeof CompressionStream !== "function") {
      return bytes;
    }
    try {
      const stream = new Blob([bytes]).stream().pipeThrough(new CompressionStream("gzip"));
      const compressed = await new Response(stream).arrayBuffer();
      return new Uint8Array(compressed);
    } catch (_) {
      return bytes;
    }
  };

  const decompressBytes = async (bytes) => {
    if (typeof DecompressionStream !== "function") {
      return bytes;
    }
    try {
      const stream = new Blob([bytes]).stream().pipeThrough(new DecompressionStream("gzip"));
      const decompressed = await new Response(stream).arrayBuffer();
      return new Uint8Array(decompressed);
    } catch (_) {
      return bytes;
    }
  };

  const encodeState = async (state) => {
    const raw = encoder.encode(JSON.stringify(state));
    const compressed = await compressBytes(raw);
    return `v2.${base64UrlEncode(compressed)}`;
  };

  const decodeState = async (payload) => {
    if (!payload || !payload.startsWith("v2.")) {
      return null;
    }
    const encoded = payload.slice(3);
    if (!encoded) {
      return null;
    }

    try {
      const bytes = base64UrlDecode(encoded);
      const maybeDecompressed = await decompressBytes(bytes);
      const parsed = JSON.parse(decoder.decode(maybeDecompressed));
      if (!parsed || typeof parsed !== "object") {
        return null;
      }
      return parsed;
    } catch (_) {
      return null;
    }
  };

  const splitLines = (value) =>
    value
      .split(/\r?\n/)
      .map((line) => line.trim())
      .filter(Boolean);

  const parseLineParts = (line, count) => {
    const parts = line.split("||").map((part) => part.trim());
    while (parts.length < count) {
      parts.push("");
    }
    return parts.slice(0, count);
  };

  const parseBlocks = (value) =>
    value
      .split(/\r?\n\s*\r?\n/)
      .map((block) => block.trim())
      .filter(Boolean)
      .map((block) => splitLines(block));

  const emptyFormState = () => ({
    basics: {
      name: "",
      phone: "",
      email: "",
      linkedin: "",
      github: "",
    },
    summary: "",
    education: "",
    experience: "",
    projects: "",
    skills: "",
  });

  const readValueFromState = (state, field) => {
    if (field.kind === "basic") {
      return typeof state?.basics?.[field.id] === "string" ? state.basics[field.id] : "";
    }
    return typeof state?.[field.id] === "string" ? state[field.id] : "";
  };

  const writeValueToState = (state, field, value) => {
    if (field.kind === "basic") {
      state.basics[field.id] = value;
      return;
    }
    state[field.id] = value;
  };

  const buildBoundary = (state) => {
    const values = fieldSchema.map((field) => readValueFromState(state, field));
    for (let attempt = 0; attempt < 50; attempt += 1) {
      const candidate = `==CVBOUNDARY_${Date.now().toString(36)}_${Math.random().toString(36).slice(2, 10)}_${attempt}==`;
      if (values.every((value) => !value.includes(candidate))) {
        return candidate;
      }
    }
    throw new Error("could not build a safe boundary marker");
  };

  const serializeTextState = (state) => {
    const boundary = buildBoundary(state);
    const lines = [textExportVersion, `boundary: ${boundary}`, ""];

    fieldSchema.forEach((field) => {
      lines.push("[field]");
      lines.push(`id: ${field.id}`);
      lines.push(`section: ${field.section}`);
      lines.push(`label: ${field.label}`);
      field.instructions.forEach((instruction) => {
        lines.push(`instruction: ${instruction}`);
      });
      lines.push(boundary);
      lines.push(readValueFromState(state, field));
      lines.push(boundary);
      lines.push("");
    });

    return lines.join("\n");
  };

  const parseTextState = (text) => {
    const normalizedText = text.replace(/\r\n?/g, "\n");
    const lines = normalizedText.split("\n");
    if (lines.length === 0) {
      throw new Error("file is empty");
    }

    lines[0] = lines[0].replace(/^\uFEFF/, "");
    if (lines[0].trim() !== textExportVersion) {
      throw new Error("unsupported text format version");
    }

    const boundaryLine = lines[1] || "";
    if (!boundaryLine.startsWith("boundary:")) {
      throw new Error("missing boundary line");
    }

    const boundary = boundaryLine.slice("boundary:".length).trim();
    if (!boundary) {
      throw new Error("boundary marker is empty");
    }

    const expectedIds = new Set(fieldSchema.map((field) => field.id));
    const valuesById = new Map();
    let index = 2;

    while (index < lines.length) {
      while (index < lines.length && lines[index].trim() === "") {
        index += 1;
      }
      if (index >= lines.length) {
        break;
      }

      if (lines[index].trim() !== "[field]") {
        throw new Error(`expected [field] at line ${index + 1}`);
      }
      index += 1;

      const metadata = { instructions: [] };
      while (index < lines.length && lines[index] !== boundary) {
        const line = lines[index];
        const separator = line.indexOf(":");
        if (separator < 0) {
          throw new Error(`invalid metadata at line ${index + 1}`);
        }

        const key = line.slice(0, separator).trim();
        const value = line.slice(separator + 1).trim();
        if (key === "instruction") {
          metadata.instructions.push(value);
        } else if (key === "id" || key === "section" || key === "label") {
          metadata[key] = value;
        } else {
          throw new Error(`unknown metadata key '${key}' at line ${index + 1}`);
        }
        index += 1;
      }

      if (index >= lines.length || lines[index] !== boundary) {
        throw new Error(`missing opening boundary for field at line ${index + 1}`);
      }
      index += 1;

      const valueLines = [];
      while (index < lines.length && lines[index] !== boundary) {
        valueLines.push(lines[index]);
        index += 1;
      }

      if (index >= lines.length || lines[index] !== boundary) {
        throw new Error(`missing closing boundary for field '${metadata.id || "unknown"}'`);
      }
      index += 1;

      if (!metadata.id) {
        throw new Error("field block is missing id");
      }
      if (!expectedIds.has(metadata.id)) {
        throw new Error(`unknown field id '${metadata.id}'`);
      }
      if (valuesById.has(metadata.id)) {
        throw new Error(`duplicate field id '${metadata.id}'`);
      }

      valuesById.set(metadata.id, valueLines.join("\n"));
    }

    const missing = fieldSchema
      .map((field) => field.id)
      .filter((id) => !valuesById.has(id));
    if (missing.length > 0) {
      throw new Error(`missing fields: ${missing.join(", ")}`);
    }

    const nextState = emptyFormState();
    fieldSchema.forEach((field) => {
      writeValueToState(nextState, field, valuesById.get(field.id) || "");
    });

    return nextState;
  };

  const toFormState = () => ({
    basics: {
      name: fields.name.value.trim(),
      phone: fields.phone.value.trim(),
      email: fields.email.value.trim(),
      linkedin: fields.linkedin.value.trim(),
      github: fields.github.value.trim(),
    },
    summary: fields.summary.value,
    education: fields.education.value,
    experience: fields.experience.value,
    projects: fields.projects.value,
    skills: fields.skills.value,
  });

  const applyFormState = (state) => {
    const basics = state?.basics || {};
    fields.name.value = basics.name || "";
    fields.phone.value = basics.phone || "";
    fields.email.value = basics.email || "";
    fields.linkedin.value = basics.linkedin || "";
    fields.github.value = basics.github || "";
    fields.summary.value = typeof state?.summary === "string" ? state.summary : "";
    fields.education.value = typeof state?.education === "string" ? state.education : "";
    fields.experience.value = typeof state?.experience === "string" ? state.experience : "";
    fields.projects.value = typeof state?.projects === "string" ? state.projects : "";
    fields.skills.value = typeof state?.skills === "string" ? state.skills : "";
    lastStateJSON = JSON.stringify(toFormState());
  };

  const downloadTextFile = (content) => {
    const blob = new Blob([content], { type: "text/plain;charset=utf-8" });
    const url = URL.createObjectURL(blob);
    const link = document.createElement("a");
    link.href = url;
    link.download = "cv.txt";
    document.body.appendChild(link);
    link.click();
    link.remove();
    URL.revokeObjectURL(url);
  };

  const toResumePayload = () => {
    const summary = splitLines(fields.summary.value);

    const education = splitLines(fields.education.value).map((line) => {
      const [school, location, degree, date] = parseLineParts(line, 4);
      return { school, location, degree, date };
    });

    const experience = parseBlocks(fields.experience.value)
      .map((lines) => {
        const [company, role, date, location, mode] = parseLineParts(lines[0] || "", 5);
        const items = lines
          .slice(1)
          .map((line) => line.replace(/^[-*]\s*/, "").trim())
          .filter(Boolean);
        return { company, role, date, location, mode, items };
      })
      .filter((entry) => entry.company || entry.role || entry.items.length > 0);

    const projects = parseBlocks(fields.projects.value)
      .map((lines) => {
        const [name, stack, date] = parseLineParts(lines[0] || "", 3);
        const items = lines
          .slice(1)
          .map((line) => line.replace(/^[-*]\s*/, "").trim())
          .filter(Boolean);
        return { name, stack, date, items };
      })
      .filter((entry) => entry.name || entry.stack || entry.items.length > 0);

    const skills = splitLines(fields.skills.value)
      .map((line) => {
        const idx = line.indexOf(":");
        if (idx < 0) {
          return { name: line, items: [] };
        }
        const name = line.slice(0, idx).trim();
        const items = line
          .slice(idx + 1)
          .split(",")
          .map((item) => item.trim())
          .filter(Boolean);
        return { name, items };
      })
      .filter((entry) => entry.name);

    return {
      basics: {
        name: fields.name.value.trim(),
        phone: fields.phone.value.trim(),
        email: fields.email.value.trim(),
        linkedin: fields.linkedin.value.trim(),
        github: fields.github.value.trim(),
      },
      summary,
      education,
      experience,
      projects,
      skills,
    };
  };

  const fromResumePayload = (resume) => {
    const basics = resume?.basics || {};
    return {
      basics: {
        name: basics.name || "",
        phone: basics.phone || "",
        email: basics.email || "",
        linkedin: basics.linkedin || "",
        github: basics.github || "",
      },
      summary: Array.isArray(resume?.summary) ? resume.summary.join("\n") : "",
      education: Array.isArray(resume?.education)
        ? resume.education
            .map((entry) => [entry.school, entry.location, entry.degree, entry.date].join(" || "))
            .join("\n")
        : "",
      experience: Array.isArray(resume?.experience)
        ? resume.experience
            .map((entry) => {
              const head = [entry.company, entry.role, entry.date, entry.location, entry.mode].join(" || ");
              const items = Array.isArray(entry.items)
                ? entry.items.map((item) => `- ${item}`).join("\n")
                : "";
              return items ? `${head}\n${items}` : head;
            })
            .join("\n\n")
        : "",
      projects: Array.isArray(resume?.projects)
        ? resume.projects
            .map((entry) => {
              const head = [entry.name, entry.stack, entry.date].join(" || ");
              const items = Array.isArray(entry.items)
                ? entry.items.map((item) => `- ${item}`).join("\n")
                : "";
              return items ? `${head}\n${items}` : head;
            })
            .join("\n\n")
        : "",
      skills: Array.isArray(resume?.skills)
        ? resume.skills
            .map((entry) => `${entry.name || ""}: ${Array.isArray(entry.items) ? entry.items.join(", ") : ""}`)
            .join("\n")
        : "",
    };
  };

  const setHashPayload = (payload) => {
    const target = payload ? `#${payload}` : "";
    if (window.location.hash === target) {
      return;
    }

    if (!target) {
      history.replaceState(null, "", `${window.location.pathname}${window.location.search}`);
      return;
    }

    history.replaceState(null, "", target);
  };

  const syncEncodedState = async () => {
    const state = toFormState();
    const nextJSON = JSON.stringify(state);
    if (nextJSON === lastStateJSON) {
      return;
    }

    lastStateJSON = nextJSON;
    const payload = await encodeState(state);
    setHashPayload(payload);
    try {
      localStorage.setItem(storageKey, payload);
    } catch (_) {
    }
  };

  const loadDefaultResume = async () => {
    const resp = await fetch("/resume/default", { headers: { Accept: "application/json" } });
    if (!resp.ok) {
      throw new Error("failed to load default resume");
    }
    return resp.json();
  };

  const bootstrap = async () => {
    const hashPayload = window.location.hash.startsWith("#") ? window.location.hash.slice(1) : "";

    if (hashPayload) {
      const decoded = await decodeState(hashPayload);
      if (decoded) {
        applyFormState(decoded);
        try {
          localStorage.setItem(storageKey, hashPayload);
        } catch (_) {
        }
        return;
      }
    }

    try {
      const cachedPayload = localStorage.getItem(storageKey) || "";
      const cachedState = await decodeState(cachedPayload);
      if (cachedState) {
        window.location.replace(`${window.location.pathname}${window.location.search}#${cachedPayload}`);
        return;
      }
    } catch (_) {
    }

    const defaults = await loadDefaultResume();
    const defaultState = fromResumePayload(defaults);
    applyFormState(defaultState);
    const payload = await encodeState(defaultState);
    setHashPayload(payload);
    try {
      localStorage.setItem(storageKey, payload);
    } catch (_) {
    }
  };

  const rehydrateFromHash = async () => {
    const hashPayload = window.location.hash.startsWith("#") ? window.location.hash.slice(1) : "";
    const decoded = await decodeState(hashPayload);
    if (!decoded) {
      return;
    }
    applyFormState(decoded);
    try {
      localStorage.setItem(storageKey, hashPayload);
    } catch (_) {
    }
  };

  form.addEventListener("submit", async (event) => {
    event.preventDefault();

    const resume = toResumePayload();
    if (!resume.basics.name) {
      status.textContent = "name is required";
      return;
    }

    generateBtn.disabled = true;
    status.textContent = "Queueing PDF generation...";

    try {
      const resp = await fetch("/pdf/generate", {
        method: "POST",
        headers: {
          "Content-Type": "application/json",
        },
        body: JSON.stringify(resume),
      });

      if (!resp.ok) {
        if (resp.status === 429) {
          status.textContent = "Queue is full. Please try again in a moment.";
          return;
        }
        throw new Error("pdf generation failed");
      }

      const queuedJob = await resp.json();
      const statusUrl = typeof queuedJob.statusUrl === "string" ? queuedJob.statusUrl : "";
      const fallbackDownloadUrl = typeof queuedJob.downloadUrl === "string" ? queuedJob.downloadUrl : "";
      if (!statusUrl) {
        throw new Error("invalid queue response");
      }

      let downloadUrl = fallbackDownloadUrl;

      while (true) {
        const statusResp = await fetch(statusUrl, {
          cache: "no-store",
        });
        if (!statusResp.ok) {
          throw new Error("failed to read job status");
        }

        const job = await statusResp.json();
        if (job.status === "queued") {
          if (typeof job.position === "number") {
            status.textContent = `Queued... position ${job.position}`;
          } else {
            status.textContent = "Queued...";
          }
        } else if (job.status === "running") {
          status.textContent = "Generating PDF...";
        } else if (job.status === "ready") {
          if (typeof job.downloadUrl === "string" && job.downloadUrl) {
            downloadUrl = job.downloadUrl;
          }
          break;
        } else if (job.status === "failed") {
          const msg = typeof job.error === "string" && job.error ? job.error : "PDF generation failed.";
          throw new Error(msg);
        } else if (job.status === "expired") {
          throw new Error("PDF job expired. Please generate again.");
        } else {
          throw new Error("unknown job status");
        }

        await new Promise((resolve) => setTimeout(resolve, 1000));
      }

      if (!downloadUrl) {
        throw new Error("download url is missing");
      }

      const downloadLink = document.createElement("a");
      downloadLink.href = `${downloadUrl}?t=${Date.now()}`;
      downloadLink.download = "cv.pdf";
      document.body.appendChild(downloadLink);
      downloadLink.click();
      downloadLink.remove();

      status.textContent = "PDF generated and downloaded.";
    } catch (error) {
      const message = error instanceof Error && error.message ? error.message : "PDF generation failed. Check your field format and try again.";
      status.textContent = message;
    } finally {
      generateBtn.disabled = false;
    }
  });

  exportTextBtn.addEventListener("click", () => {
    try {
      const text = serializeTextState(toFormState());
      downloadTextFile(text);
      status.textContent = "CV text exported.";
    } catch (err) {
      status.textContent = `Export failed: ${err instanceof Error ? err.message : "unknown error"}`;
    }
  });

  importTextBtn.addEventListener("click", () => {
    importTextFile.click();
  });

  importTextFile.addEventListener("change", async (event) => {
    const input = event.target;
    const file = input.files && input.files[0] ? input.files[0] : null;
    if (!file) {
      return;
    }

    try {
      const text = await file.text();
      const importedState = parseTextState(text);
      applyFormState(importedState);
      await syncEncodedState();
      status.textContent = "CV text imported.";
    } catch (err) {
      status.textContent = `Import failed: ${err instanceof Error ? err.message : "unknown error"}`;
    } finally {
      input.value = "";
    }
  });

  window.addEventListener("hashchange", () => {
    void rehydrateFromHash();
  });

  window.setInterval(() => {
    void syncEncodedState();
  }, 700);

  void bootstrap().catch(() => {
    status.textContent = "Failed to load defaults.";
  });
})();

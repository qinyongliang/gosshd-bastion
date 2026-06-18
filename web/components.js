export function html(strings, ...values) {
  return strings.reduce((out, part, index) => out + part + escapeHTML(values[index] ?? ""), "");
}

export function raw(value) {
  return { __raw: String(value ?? "") };
}

export function escapeHTML(value) {
  if (value && value.__raw) return value.__raw;
  return String(value ?? "")
    .replaceAll("&", "&amp;")
    .replaceAll("<", "&lt;")
    .replaceAll(">", "&gt;")
    .replaceAll('"', "&quot;")
    .replaceAll("'", "&#039;");
}

export function badge(text, tone = "neutral") {
  return raw(`<span class="badge ${tone}">${escapeHTML(text)}</span>`);
}

export function emptyState(title, body) {
  return raw(`
    <div class="empty-state">
      <div class="empty-orbit"></div>
      <strong>${escapeHTML(title)}</strong>
      <span>${escapeHTML(body)}</span>
    </div>
  `);
}

export function field(label, name, attrs = {}) {
  const tag = attrs.multiline ? "textarea" : "input";
  const value = attrs.value ?? "";
  const type = attrs.type || "text";
  const placeholder = attrs.placeholder || "";
  const required = attrs.required ? "required" : "";
  const extra = attrs.autocomplete ? `autocomplete="${escapeHTML(attrs.autocomplete)}"` : "";
  const control =
    tag === "textarea"
      ? `<textarea name="${escapeHTML(name)}" placeholder="${escapeHTML(placeholder)}" ${required}>${escapeHTML(value)}</textarea>`
      : `<input name="${escapeHTML(name)}" type="${escapeHTML(type)}" value="${escapeHTML(value)}" placeholder="${escapeHTML(placeholder)}" ${required} ${extra} />`;
  return raw(`<label class="field"><span>${escapeHTML(label)}</span>${control}</label>`);
}

export function selectField(label, name, options, selected = "") {
  const choices = options
    .map((option) => {
      const value = typeof option === "string" ? option : option.value;
      const text = typeof option === "string" ? option : option.label;
      return `<option value="${escapeHTML(value)}" ${value === selected ? "selected" : ""}>${escapeHTML(text)}</option>`;
    })
    .join("");
  return raw(`<label class="field"><span>${escapeHTML(label)}</span><select name="${escapeHTML(name)}">${choices}</select></label>`);
}

export function formData(form) {
  return Object.fromEntries(new FormData(form).entries());
}

export function icon(name) {
  const paths = {
    key: "M21 2l-2 2m-7.5 7.5a5 5 0 1 1-7.07 7.07 5 5 0 0 1 7.07-7.07Zm0 0L15 8m0 0 2 2 4-4-2-2-4 4Z",
    plus: "M12 5v14M5 12h14",
    server: "M4 6a2 2 0 0 1 2-2h12a2 2 0 0 1 2 2v4H4V6Zm0 8h16v4a2 2 0 0 1-2 2H6a2 2 0 0 1-2-2v-4Zm4-6h.01M8 16h.01",
    shield: "M12 22s8-4 8-10V5l-8-3-8 3v7c0 6 8 10 8 10Z",
    log: "M8 6h13M8 12h13M8 18h13M3 6h.01M3 12h.01M3 18h.01",
    spark: "M12 2l1.8 6.2L20 10l-6.2 1.8L12 18l-1.8-6.2L4 10l6.2-1.8L12 2Z",
    copy: "M8 8h10v12H8V8Zm-4 8V4h10",
  };
  return raw(`<svg viewBox="0 0 24 24" aria-hidden="true"><path d="${paths[name] || paths.spark}"/></svg>`);
}

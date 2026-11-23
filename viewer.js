// SPDX-License-Identifier: Apache-2.0
function button(label, click) {
  const element = document.createElement("button");
  element.textContent = label;
  element.addEventListener("click", click);
  return element;
}

function paginate(list) {
  const items = Array.from(list.children);
  const size = Number(list.dataset.page);
  let page = 0;
  let filter = () => true;
  const nav = document.createElement("nav");
  nav.className = "pager";
  const label = document.createElement("span");
  label.setAttribute("aria-live", "polite");
  const prev = button("上一页", () => { page--; render(); });
  const next = button("下一页", () => { page++; render(); });
  nav.append(prev, label, next);
  list.after(nav);

  function render() {
    const matches = items.filter(filter);
    const pages = Math.ceil(matches.length / size);
    page = Math.max(0, Math.min(page, pages - 1));
    const visible = new Set(matches.slice(page * size, (page + 1) * size));
    items.forEach(item => { item.hidden = !visible.has(item); });
    nav.hidden = pages <= 1;
    label.textContent = (page + 1) + " / " + pages;
    prev.disabled = page === 0;
    next.disabled = page + 1 >= pages;
  }

  render();
  return (predicate, reset = true) => {
    filter = predicate;
    if (reset) page = 0;
    render();
  };
}

const pagers = new Map(Array.from(document.querySelectorAll("[data-page]"),
  list => [list, paginate(list)]));
const search = document.querySelector("#repo-search");
if (search) {
  search.addEventListener("input", () => {
    const term = search.value.trim().toLowerCase();
    pagers.get(document.querySelector("#repos"))(item => item.dataset.path.toLowerCase().includes(term));
  });
}

const findings = document.querySelector("#findings");
if (findings) {
  const cards = Array.from(findings.children);
  const severity = document.querySelector("#severity");
  const category = document.querySelector("#category");
  const hide = document.querySelector("#hide-marked");
  const prefix = "btcr:" + location.pathname + ":";
  const marks = new Map();
  // Browsers with storage disabled can still track marks until the page closes.
  let storage;
  try { storage = window.localStorage; } catch {}
  hide.checked = storage?.getItem("btcr:hide-marked") !== "false";
  for (const card of cards) {
    const saved = storage?.getItem(prefix + card.dataset.mark);
    if (saved) marks.set(card.dataset.mark, saved);
  }

  function refresh(reset) {
    let hidden = 0;
    pagers.get(findings)(card => {
      const mark = marks.get(card.dataset.mark);
      card.querySelector("[data-state]").textContent = mark === "fixed" ? "已修复" : mark === "ignored" ? "已忽略" : "";
      const matches = (!severity.value || severity.value === card.dataset.severity) &&
        (!category.value || category.value === card.dataset.category);
      if (matches && hide.checked && mark) hidden++;
      return matches && !(hide.checked && mark);
    }, reset);
    document.querySelector("#mark-count").textContent = "已标记 " + marks.size + " · 已隐藏 " + hidden;
  }
  for (const select of [severity, category]) {
    const counts = new Map();
    for (const card of cards) {
      const value = card.dataset[select.id];
      if (value) counts.set(value, (counts.get(value) || 0) + 1);
    }
    for (const [value, count] of counts) select.add(new Option(value + " (" + count + ")", value));
    select.addEventListener("change", () => refresh(true));
  }
  function setMark(key, value) {
    if (value) marks.set(key, value);
    else marks.delete(key);
    try {
      if (value) storage?.setItem(prefix + key, value);
      else storage?.removeItem(prefix + key);
    } catch {}
  }
  findings.addEventListener("click", event => {
    const target = event.target.closest("button[data-value]");
    if (!target) return;
    setMark(target.closest("[data-mark]").dataset.mark, target.dataset.value);
    refresh(false);
  });
  hide.addEventListener("change", () => {
    try { storage?.setItem("btcr:hide-marked", String(hide.checked)); } catch {}
    refresh(false);
  });
  document.querySelector("#clear-marks").addEventListener("click", () => {
    for (const key of marks.keys()) setMark(key, "");
    refresh(false);
  });
  refresh(true);
}

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
  (list.closest("table") || list).after(nav);

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
  return predicate => {
    filter = predicate;
    page = 0;
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
  function filterFindings() {
    pagers.get(findings)(card =>
      (!severity.value || severity.value === card.dataset.severity) &&
      (!category.value || category.value === card.dataset.category));
  }
  for (const select of [severity, category]) {
    const counts = new Map();
    for (const card of cards) {
      const value = card.dataset[select.id];
      if (value) counts.set(value, (counts.get(value) || 0) + 1);
    }
    for (const [value, count] of counts) select.add(new Option(value + " (" + count + ")", value));
    select.addEventListener("change", filterFindings);
  }
  filterFindings();
}

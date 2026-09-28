const assert = require("node:assert/strict");
const fs = require("node:fs");
const vm = require("node:vm");

const source = fs.readFileSync(process.argv[2], "utf8");
const requests = [];
const storage = new Map();
class Element {
  constructor(tagName) { this.tagName = tagName; this.children = []; this.attributes = {}; this.handlers = {}; this._text = ""; }
  set textContent(value) { this._text = String(value); this.children = []; }
  get textContent() { return this._text + this.children.map((child) => child.textContent).join(""); }
  appendChild(child) { this.children.push(child); return child; }
  append(...children) { children.forEach((child) => this.appendChild(child)); }
  replaceChildren(...children) { this.children = []; this._text = ""; children.forEach((child) => this.appendChild(child)); }
  setAttribute(name, value) { this.attributes[name] = String(value); }
  addEventListener(name, handler) { this.handlers[name] = handler; }
}
const location = { origin: "https://clio.example", pathname: "/collections/pool/measurements", search: "?page=2" };
const context = {
  URLSearchParams,
  URL,
  AbortController,
  location,
  history: {
    pushState(_state, _title, value) { Object.assign(location, { pathname: new URL(value, location.origin).pathname, search: new URL(value, location.origin).search }); },
    replaceState(_state, _title, value) { Object.assign(location, { pathname: new URL(value, location.origin).pathname, search: new URL(value, location.origin).search }); }
  },
  document: { createElement: (name) => new Element(name), querySelector: () => null, documentElement: new Element("html") },
  localStorage: { getItem: (key) => storage.get(key) || null, setItem: (key, value) => storage.set(key, value) },
  ClioMarkdown: { render: (source) => `<p>${source}</p>` },
  fetch: async function (url, options = {}) {
    assert.ok(this.window && this.location && this.history, "fetch is called with the browser window as receiver");
    requests.push({ url: String(url), options });
    const parsed = new URL(String(url));
    const method = options.method || "GET";
    let body;
    let status = 200;
    if (parsed.pathname.endsWith("/records") && method === "GET") {
      const offset = Number(parsed.searchParams.get("offset") || 0);
      body = offset === 0
        ? { data: [{ id: "1", amount: "10.20" }, { id: "2", amount: "20.30" }], page: { limit: 2, offset: 0, count: 2, total: 3 } }
        : { data: [{ id: "3", amount: "30.40" }], page: { limit: 2, offset: 2, count: 1, total: 3 } };
    } else if (parsed.pathname.endsWith("/records/missing")) {
      status = 404;
      body = { error: "not_found", message: "Record not found" };
    } else if (method === "POST" || method === "PATCH") {
      body = JSON.parse(options.body);
    } else if (parsed.pathname.endsWith("/pages")) {
      body = { path: parsed.searchParams.get("path"), content: "# Hi", content_type: "text/markdown" };
    } else if (parsed.pathname.endsWith("/directories")) {
      body = { path: parsed.searchParams.get("path"), children: [] };
    } else if (parsed.pathname.endsWith("/metadata")) {
      body = { api_version: "v1", groups: [] };
    } else if (parsed.pathname.endsWith("/groups/pool/tables/measurements")) {
      body = { group: "pool", name: "measurements", label: "Measurements", kind: "timeseries", fields: [{ name: "amount", label: "Amount", type: "decimal" }, { name: "hidden", label: "Hidden", hidden: true }] };
    } else if (parsed.pathname.endsWith("/groups/pool/tables")) {
      body = [{ name: "measurements" }];
    } else if (parsed.pathname.endsWith("/groups/pool")) {
      body = { name: "pool" };
    } else if (parsed.pathname.endsWith("/groups")) {
      body = [{ name: "pool" }];
    } else {
      body = {};
    }
    return {
      ok: status >= 200 && status < 300,
      status,
      text: async () => body == null ? "" : JSON.stringify(body)
    };
  }
};
context.window = context;
vm.runInNewContext(source, context, { filename: "clio.js" });

async function main() {
  const Clio = context.Clio;
  assert.equal(Clio.version, "1.0.0");
  assert.equal(Clio.apiVersion, "v1");
  assert.equal(typeof Clio.DataBrowser.mount, "function");
  assert.equal(Clio.Markdown.render("Hello"), "<p>Hello</p>");

  const clio = new Clio();
  assert.equal((await clio.metadata()).api_version, "v1");
  assert.equal((await clio.groups())[0].name, "pool");
  assert.equal((await clio.group("pool").get()).name, "pool");
  assert.equal((await clio.group("pool").tables())[0].name, "measurements");
  assert.equal((await clio.group("pool").table("measurements").metadata()).kind, "timeseries");
  assert.equal((await clio.createGroup({ name: "garage" })).name, "garage");
  assert.deepEqual(JSON.parse(requests.at(-1).options.body), { name: "garage" });
  assert.equal((await clio.group("pool").createTable({ name: "notes", fields: [] })).name, "notes");
  assert.equal(requests.at(-1).url.endsWith("/groups/pool/tables"), true);

  const signal = new AbortController().signal;
  const table = clio.table("pool", "measurements");
  const firstPage = await table.query({
    limit: 2,
    filter: { "amount.gte": "10.00", "status.in": ["open", "closed"] },
    signal
  });
  assert.equal(firstPage.data[0].amount, "10.20", "decimal values stay strings");
  const queryRequest = requests.find((request) => request.url.includes("filter.amount.gte"));
  const queryURL = new URL(queryRequest.url);
  assert.deepEqual(queryURL.searchParams.getAll("filter.status.in"), ["open", "closed"]);
  assert.equal(queryURL.searchParams.get("filter.amount.gte"), "10.00");
  assert.equal(queryRequest.options.signal, signal);

  const rows = [];
  for await (const row of table.records({ limit: 2, sort: "amount", order: "asc" })) rows.push(row);
  assert.deepEqual(rows.map((row) => row.id), ["1", "2", "3"]);
  const pages = requests.filter((request) => request.url.includes("/records?") && request.url.includes("sort=amount"));
  assert.equal(pages.length, 2);
  assert.equal(new URL(pages[1].url).searchParams.get("offset"), "2");

  const created = await table.create({ amount: "42.10" });
  assert.equal(created.amount, "42.10");
  assert.equal(requests.at(-1).options.method, "POST");
  assert.equal(requests.at(-1).options.headers["Content-Type"], "application/json");
  assert.equal((await table.update("record-123", { amount: null })).amount, null);
  assert.match(requests.at(-1).url, /records\/record-123$/);
  await table.delete("123");
  assert.equal(requests.at(-1).options.method, "DELETE");

  assert.equal((await clio.page("/reports/latest.md")).content, "# Hi");
  assert.equal((await clio.directory("/reports")).path, "/reports");

  const browserHost = new Element("div");
  const browser = Clio.DataBrowser.mount(browserHost, { pageSize: 2 });
  await browser.ready;
  const browserText = browserHost.textContent;
  assert.match(browserText, /Measurements/);
  assert.match(browserText, /30\.40/);
  assert.doesNotMatch(browserText, /Hidden/);
  const tableViewLink = browserHost.children[0].children[1];
  assert.equal(tableViewLink.href, "/t/pool/measurements");
  assert.equal(tableViewLink.hidden, false);
  const themeToggle = browserHost.children[0].children[2];
  assert.equal(themeToggle.attributes["aria-label"], "Switch to dark theme");
  themeToggle.handlers.click();
  assert.equal(context.document.documentElement.attributes["data-theme"], "dark");
  assert.equal(storage.get("clio-data-browser-theme"), "dark");
  const browserRequest = requests.find((request) => request.url.includes("/records?") && new URL(request.url).searchParams.get("offset") === "2");
  assert.ok(browserRequest, "browser loads the selected page through ClioJS");
  assert.equal(location.pathname, "/collections/pool/measurements");
  assert.equal(new URLSearchParams(location.search).get("page"), "2");
  browser.destroy();

  let error;
  try {
    await table.get("missing");
  } catch (caught) {
    error = caught;
  }
  assert.equal(error.status, 404);
  assert.equal(error.code, "not_found");
  assert.equal(error.message, "Record not found");
}

main().catch((error) => {
  console.error(error);
  process.exitCode = 1;
});

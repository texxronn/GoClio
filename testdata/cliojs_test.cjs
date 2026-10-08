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
function collectByTag(element, tag) {
  const found = [];
  if (element.tagName === tag) found.push(element);
  for (const child of element.children || []) found.push(...collectByTag(child, tag));
  return found;
}
const location = { origin: "https://clio.example", pathname: "/default/data", search: "?group=pool&table=measurements&page=2" };
const context = {
  URLSearchParams,
  URL,
  AbortController,
  location,
  history: {
    pushState(_state, _title, value) { Object.assign(location, { pathname: new URL(value, location.origin).pathname, search: new URL(value, location.origin).search }); },
    replaceState(_state, _title, value) { Object.assign(location, { pathname: new URL(value, location.origin).pathname, search: new URL(value, location.origin).search }); }
  },
  document: {
    createElement: (name) => new Element(name),
    createTextNode: (text) => { const node = new Element("#text"); node.textContent = String(text); return node; },
    querySelector: () => null,
    documentElement: new Element("html")
  },
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
    } else if (parsed.pathname.endsWith("/search")) {
      body = {
        data: [{ id: "file-1", path: "/reports/latest.md", kind: "page", content_type: "text/markdown", source: "native", snippet: "the \u27e6needle\u27e7 is here", score: 1.1 }],
        page: { limit: 100, offset: 0, count: 1, total: 1 }
      };
    } else if (parsed.pathname.endsWith("/files") && method === "DELETE" && parsed.searchParams.get("path") === "/protected.txt") {
      status = 409;
      body = { error: "conflict", message: "File is referenced by an attachment" };
    } else if (parsed.pathname.endsWith("/files")) {
      const p = parsed.searchParams.get("path");
      if (p === "/reports/latest.md") {
        body = { id: "file-1", path: p, kind: "page", content_type: "text/markdown" };
      } else if (p === "/") {
        body = {
          path: "/", kind: "directory",
          children: [
            { path: "/docs", name: "docs", kind: "directory", url: "https://clio.example/default/files/docs" },
            { path: "/note.txt", name: "note.txt", kind: "file", id: "file-2", content_type: "text/plain", size: 3, url: "https://clio.example/default/files/note.txt" }
          ]
        };
      } else if (p === "/docs") {
        body = {
          path: "/docs", kind: "directory",
          children: [
            { path: "/docs/old.txt", name: "old.txt", kind: "file", id: "file-3", content_type: "text/plain", size: 1, url: "https://clio.example/default/files/docs/old.txt" }
          ]
        };
      } else {
        body = { path: p, kind: "directory", children: [] };
      }
    } else if (parsed.pathname.endsWith("/content")) {
      body = "# Hi";
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
  assert.equal(Clio.version, "1.1.0");
  assert.equal(Clio.apiVersion, "v1");
  assert.equal(typeof Clio.DataBrowser.mount, "function");
  assert.equal(typeof Clio.FileBrowser.mount, "function");
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
  await clio.files();
  assert.equal(new URL(requests.at(-1).url).searchParams.has("path"), false, "files() lists the flat catalog");
  await clio.getFile("file-1");
  assert.match(requests.at(-1).url, /\/files\/file-1$/);
  const searchResult = await clio.search("needle");
  assert.equal(new URL(requests.at(-1).url).searchParams.get("q"), "needle");
  assert.match(searchResult.data[0].snippet, /\u27e6needle\u27e7/);

  await clio.publishPage({ path: "/reports/new.md", content: "# New", content_type: "text/markdown" });
  assert.equal(requests.at(-1).options.method, "PUT");
  assert.match(requests.at(-1).url, /\/files\?path=%2Freports%2Fnew\.md$/);
  assert.equal(requests.at(-1).options.body, "# New");
  assert.equal(requests.at(-1).options.headers["Content-Type"], "text/markdown");
  await clio.deletePage("/reports/new.md");
  assert.equal(requests.at(-1).options.method, "DELETE");
  assert.match(requests.at(-1).url, /\/files\?path=%2Freports%2Fnew\.md$/);

  const browserHost = new Element("div");
  const browser = Clio.DataBrowser.mount(browserHost, { pageSize: 2 });
  await browser.ready;
  const browserText = browserHost.textContent;
  assert.match(browserText, /Measurements/);
  assert.match(browserText, /30\.40/);
  assert.doesNotMatch(browserText, /Hidden/);
  const tableViewLink = browserHost.children[0].children[1];
  assert.equal(tableViewLink.href, "/default/data/pool/measurements");
  assert.equal(tableViewLink.hidden, false);
  const themeToggle = browserHost.children[0].children[2];
  assert.equal(themeToggle.attributes["aria-label"], "Switch to dark theme");
  themeToggle.handlers.click();
  assert.equal(context.document.documentElement.attributes["data-theme"], "dark");
  assert.equal(storage.get("clio-data-browser-theme"), "dark");
  const browserRequest = requests.find((request) => request.url.includes("/records?") && new URL(request.url).searchParams.get("offset") === "2");
  assert.ok(browserRequest, "browser loads the selected page through ClioJS");
  assert.equal(location.pathname, "/default/data");
  const browserParams = new URLSearchParams(location.search);
  assert.equal(browserParams.get("group"), "pool");
  assert.equal(browserParams.get("table"), "measurements");
  assert.equal(browserParams.get("page"), "2");
  browser.destroy();

  // FileBrowser: gutter navigation, URL state, light actions and search.
  location.pathname = "/default/files";
  location.search = "";
  const fileHost = new Element("div");
  const fileBrowser = Clio.FileBrowser.mount(fileHost, { path: "/" });
  await fileBrowser.ready;
  assert.equal(fileBrowser.path, "/");
  assert.equal(location.pathname, "/default/files");
  assert.match(fileHost.textContent, /docs/);
  assert.match(fileHost.textContent, /note\.txt/);
  assert.match(fileHost.textContent, /Breadcrumb|default/);
  assert.ok(requests.some((request) => request.url.endsWith("/files?path=%2F")), "the root directory is listed");

  await fileBrowser.navigate("/docs");
  assert.equal(fileBrowser.path, "/docs");
  assert.equal(location.pathname, "/default/files/docs");
  assert.match(fileHost.textContent, /old\.txt/);

  await fileBrowser.createFolder("new");
  const createRequest = requests.filter((request) => request.url.endsWith("/files/directories")).at(-1);
  assert.ok(createRequest, "create folder uses the directories endpoint");
  assert.deepEqual(JSON.parse(createRequest.options.body), { path: "/docs/new" });

  await fileBrowser.upload("hello", "up.txt");
  const uploadRequest = requests.filter((request) => request.options.method === "PUT" && request.url.includes("path=%2Fdocs%2Fup.txt")).at(-1);
  assert.ok(uploadRequest, "upload puts raw bytes at the target path");
  assert.equal(uploadRequest.options.body, "hello");

  await fileBrowser.rename("/docs/old.txt", "/docs/new.txt");
  const moveRequest = requests.filter((request) => request.url.endsWith("/files/move")).at(-1);
  assert.ok(moveRequest, "rename uses the files move endpoint");
  assert.deepEqual(JSON.parse(moveRequest.options.body), { from: "/docs/old.txt", to: "/docs/new.txt" });

  await fileBrowser.remove("/docs/gone.txt");
  const deleteRequest = requests.filter((request) => request.options.method === "DELETE").at(-1);
  assert.match(deleteRequest.url, /\/files\?path=%2Fdocs%2Fgone\.txt$/);

  await fileBrowser.remove("/protected.txt");
  assert.match(fileHost.textContent, /referenced by an attachment/, "a 409 conflict message is surfaced");

  await fileBrowser.search("needle");
  const searchRequest = requests.filter((request) => request.url.includes("/search?")).at(-1);
  assert.ok(searchRequest, "search uses the search API");
  assert.equal(new URL(searchRequest.url).searchParams.get("q"), "needle");
  assert.match(fileHost.textContent, /needle/);
  assert.equal(collectByTag(fileHost, "mark").length, 1, "the search snippet highlights the matched term");
  fileBrowser.destroy();

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

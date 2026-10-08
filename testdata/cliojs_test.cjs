const assert = require("node:assert/strict");
const fs = require("node:fs");
const vm = require("node:vm");

const source = fs.readFileSync(process.argv[2], "utf8");
const requests = [];
const storage = new Map();
const session = new Map();
const navigation = [];
const opened = [];
const docHandlers = {};
class Element {
  constructor(tagName) {
    this.tagName = tagName;
    this.children = [];
    this.attributes = {};
    this.handlers = {};
    this._text = "";
    this.style = {};
    this.hidden = false;
    this.parentNode = null;
  }
  set textContent(value) { this._text = String(value); this.children = []; }
  get textContent() { return this._text + this.children.map((child) => child.textContent).join(""); }
  appendChild(child) { this.children.push(child); if (child) child.parentNode = this; return child; }
  append(...children) { children.forEach((child) => this.appendChild(child)); }
  replaceChildren(...children) { this.children = []; this._text = ""; children.forEach((child) => this.appendChild(child)); }
  removeChild(child) { this.children = this.children.filter((node) => node !== child); if (child) child.parentNode = null; return child; }
  remove() { if (this.parentNode) this.parentNode.removeChild(this); }
  contains(node) { if (this === node) return true; return this.children.some((child) => child.contains && child.contains(node)); }
  setAttribute(name, value) { this.attributes[name] = String(value); }
  getAttribute(name) { return Object.prototype.hasOwnProperty.call(this.attributes, name) ? this.attributes[name] : null; }
  removeAttribute(name) { delete this.attributes[name]; }
  addEventListener(name, handler) { this.handlers[name] = handler; }
  focus() { context.document.activeElement = this; }
  getBoundingClientRect() { return { left: 0, top: 0, right: 0, bottom: 0, width: 0, height: 0, x: 0, y: 0 }; }
}
function collectByTag(element, tag) {
  const found = [];
  if (element.tagName === tag) found.push(element);
  for (const child of element.children || []) found.push(...collectByTag(child, tag));
  return found;
}
const location = {
  origin: "https://clio.example",
  pathname: "/default/data",
  search: "?group=pool&table=measurements&page=2",
  assign(value) { navigation.push(String(value)); }
};
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
    documentElement: new Element("html"),
    activeElement: null,
    addEventListener(name, handler) { (docHandlers[name] = docHandlers[name] || []).push(handler); },
    removeEventListener(name, handler) { docHandlers[name] = (docHandlers[name] || []).filter((entry) => entry !== handler); },
    dispatchEvent(name, event) { for (const handler of (docHandlers[name] || []).slice()) handler(event); }
  },
  open: (url) => { opened.push(String(url)); },
  localStorage: { getItem: (key) => storage.get(key) || null, setItem: (key, value) => storage.set(key, value) },
  sessionStorage: { getItem: (key) => session.get(key) || null, setItem: (key, value) => session.set(key, value) },
  ClioMarkdown: { render: (source) => `<p>${source}</p>` },
  fetch: async function (url, options = {}) {
    assert.ok(this.window && this.location && this.history, "fetch is called with the browser window as receiver");
    requests.push({ url: String(url), options });
    const parsed = new URL(String(url));
    const method = options.method || "GET";
    let body;
    let status = 200;
    if (parsed.pathname === "/api/v1/projects" && method === "GET") {
      body = [
        { name: "default", label: "Default", url: "https://clio.example/default/", api_url: "https://clio.example/api/v1/projects/default" },
        { name: "bills", label: "Bills", url: "https://clio.example/bills/", api_url: "https://clio.example/api/v1/projects/bills" }
      ];
    } else if (parsed.pathname === "/api/v1/projects" && method === "POST") {
      body = JSON.parse(options.body);
      body.name = String(body.name).toLowerCase();
      status = 201;
    } else if (parsed.pathname.startsWith("/api/v1/projects/") && method === "DELETE") {
      const name = decodeURIComponent(parsed.pathname.slice("/api/v1/projects/".length));
      if (name === "busy") {
        status = 409;
        body = { error: "conflict", message: "Project is not empty" };
      } else if (name === "default") {
        status = 422;
        body = { error: "validation_error", message: "The default project cannot be deleted" };
      } else {
        status = 204;
        body = null;
      }
    } else if (parsed.pathname.endsWith("/records") && method === "GET") {
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
    } else if (parsed.pathname.endsWith("/files") && method === "PUT") {
      const p = parsed.searchParams.get("path");
      if (p === "/bad.txt") {
        status = 500;
        body = { error: "server_error", message: "Upload failed" };
      } else {
        body = { path: p, kind: "file", id: "uploaded", content_type: options.headers["Content-Type"] || "application/octet-stream" };
      }
    } else if (parsed.pathname.endsWith("/files")) {
      const p = parsed.searchParams.get("path");
      const offset = Number(parsed.searchParams.get("offset") || 0);
      if (p === "/reports/latest.md") {
        body = { id: "file-1", path: p, kind: "page", content_type: "text/markdown" };
      } else if (p === "/") {
        body = {
          path: "/", kind: "directory",
          children: [
            { path: "/docs", name: "docs", kind: "directory", url: "https://clio.example/default/files/docs" },
            { path: "/note.txt", name: "note.txt", kind: "file", id: "file-2", content_type: "text/plain", size: 3, created_at: "2026-01-02T03:04:05Z", updated_at: "2026-02-03T04:05:06Z", url: "https://clio.example/default/files/note.txt" },
            { path: "/a", name: "a", kind: "directory", url: "https://clio.example/default/files/a" },
            { path: "/big", name: "big", kind: "directory", url: "https://clio.example/default/files/big" }
          ],
          page: { limit: 100, offset: 0, count: 4, total: 4 }
        };
      } else if (p === "/docs") {
        body = {
          path: "/docs", kind: "directory",
          children: [
            { path: "/docs/old.txt", name: "old.txt", kind: "file", id: "file-3", content_type: "text/plain", size: 1, created_at: "2025-12-01T00:00:00Z", updated_at: "2025-12-02T00:00:00Z", url: "https://clio.example/default/files/docs/old.txt" },
            { path: "/docs/nested", name: "nested", kind: "directory", url: "https://clio.example/default/files/docs/nested" }
          ],
          page: { limit: 100, offset: 0, count: 2, total: 2 }
        };
      } else if (p === "/a") {
        body = {
          path: "/a", kind: "directory",
          children: [
            { path: "/a/b", name: "b", kind: "directory", url: "https://clio.example/default/files/a/b" }
          ],
          page: { limit: 100, offset: 0, count: 1, total: 1 }
        };
      } else if (p === "/big") {
        body = offset === 0
          ? { path: "/big", kind: "directory", children: [{ path: "/big/one", name: "one", kind: "directory", url: "https://clio.example/default/files/big/one" }], page: { limit: 1000, offset: 0, count: 1, total: 2 } }
          : { path: "/big", kind: "directory", children: [{ path: "/big/two", name: "two", kind: "directory", url: "https://clio.example/default/files/big/two" }], page: { limit: 1000, offset: 1, count: 1, total: 2 } };
      } else {
        body = { path: p, kind: "directory", children: [], page: { limit: 100, offset: 0, count: 0, total: 0 } };
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
  assert.equal(Clio.version, "1.4.0");
  assert.equal(Clio.apiVersion, "v1");
  assert.equal(typeof Clio.DataBrowser.mount, "function");
  assert.equal(typeof Clio.FileBrowser.mount, "function");
  assert.equal(typeof Clio.Projects.mount, "function");
  assert.equal(typeof Clio.Theme.mount, "function");
  assert.equal(typeof Clio.Theme.toggle, "function");
  assert.equal(typeof Clio.Theme.set, "function");
  assert.equal(typeof Clio.Theme.current, "function");
  assert.equal(typeof Clio.Theme.effective, "function");
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
  const toolbar = browserHost.children[0];
  assert.equal(toolbar.children[0].className, "project-switcher", "the project switcher is first in the toolbar");
  const tableViewLink = toolbar.children[2];
  assert.equal(tableViewLink.href, "/default/data/pool/measurements");
  assert.equal(tableViewLink.hidden, false);
  const themeToggle = toolbar.children[3];
  assert.equal(themeToggle.attributes["aria-label"], "Switch to dark theme");
  themeToggle.handlers.click();
  assert.equal(context.document.documentElement.attributes["data-theme"], "dark");
  assert.equal(storage.get("clio-theme"), "dark", "the toggle writes the shared clio-theme key");
  assert.equal(storage.has("clio-data-browser-theme"), false, "the old bespoke theme key is no longer used");
  assert.equal(Clio.Theme.current(), "dark");
  assert.equal(Clio.Theme.effective(), "dark");
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

  // Unified theme: one controller and one storage key shared by the
  // DataBrowser and FileBrowser, with no per-page data-theme writes.
  assert.equal(storage.get("clio-theme"), "dark", "the DataBrowser toggle persisted the shared key");
  assert.equal(Clio.Theme.toggle(), "light");
  assert.equal(context.document.documentElement.attributes["data-theme"], "light");
  assert.equal(storage.get("clio-theme"), "light");
  assert.equal(Clio.Theme.set("dark"), "dark");
  assert.equal(context.document.documentElement.attributes["data-theme"], "dark");
  const sharedThemeHost = new Element("div");
  const sharedThemeButton = new Element("button");
  sharedThemeHost.appendChild(sharedThemeButton);
  const themeMount = Clio.Theme.mount(sharedThemeButton);
  assert.equal(sharedThemeButton.attributes["aria-label"], "Switch to light theme");
  sharedThemeButton.handlers.click();
  assert.equal(context.document.documentElement.attributes["data-theme"], "light", "a mounted toggle flips the shared document theme");
  assert.equal(Clio.Theme.current(), "light");
  themeMount.destroy();

  // --- FileBrowser redesign: lazy tree, staged upload, keyboard, no grid ---
  async function settle() {
    for (let i = 0; i < 200; i++) await Promise.resolve();
  }
  function nodeByPath(host, path) {
    return collectByTag(host, "div").find((el) => el.attributes && el.attributes["data-path"] === path);
  }
  function focusedTreePath(host) {
    const el = collectByTag(host, "div").find((node) => node.className && node.className.includes("fb-tree-item") && node.tabIndex === 0);
    return el && el.attributes["data-path"];
  }
  function buttonByClass(host, className) {
    return collectByTag(host, "button").find((button) => button.className === className);
  }

  session.clear();
  location.pathname = "/default/files";
  location.search = "";
  const treeHost = new Element("div");
  const treeBrowser = Clio.FileBrowser.mount(treeHost, { path: "/" });
  await treeBrowser.ready;

  const tree = collectByTag(treeHost, "div").find((el) => el.attributes.role === "tree");
  assert.ok(tree, "the file browser renders a tree");
  assert.equal(tree.attributes["aria-label"], "Folder tree");
  assert.equal(nodeByPath(treeHost, "/").attributes.role, "treeitem");
  assert.equal(nodeByPath(treeHost, "/").attributes["aria-level"], "1");
  assert.equal(nodeByPath(treeHost, "/").attributes["aria-selected"], "true", "the current path is highlighted");
  assert.ok(collectByTag(nodeByPath(treeHost, "/"), "button").some((button) => button.className === "fb-tree-label" && button.textContent === "default"));
  assert.ok(!tree.textContent.includes("note.txt"), "files are not tree nodes");
  assert.equal(collectByTag(treeHost, "div").filter((el) => el.className && el.className.includes("grid")).length, 0, "there is no grid view");
  assert.doesNotMatch(treeHost.textContent, /grid view/i);

  assert.equal(nodeByPath(treeHost, "/docs").attributes["aria-expanded"], "false");
  const treeFetches = (path) => requests.filter((r) => r.url.includes(`path=${encodeURIComponent(path)}`) && r.url.includes("limit=1000")).length;
  assert.equal(treeFetches("/docs"), 0, "collapsed nodes are not fetched");
  buttonByClass(nodeByPath(treeHost, "/docs"), "fb-tree-toggle").handlers.click();
  await settle();
  assert.equal(treeFetches("/docs"), 1, "expanding fetches the node's children once");
  assert.equal(nodeByPath(treeHost, "/docs").attributes["aria-expanded"], "true");
  assert.ok(nodeByPath(treeHost, "/docs/nested"), "expanded children render");
  buttonByClass(nodeByPath(treeHost, "/docs"), "fb-tree-toggle").handlers.click();
  await settle();
  assert.equal(nodeByPath(treeHost, "/docs/nested"), undefined, "collapsing hides children");
  buttonByClass(nodeByPath(treeHost, "/docs"), "fb-tree-toggle").handlers.click();
  await settle();
  assert.equal(treeFetches("/docs"), 1, "re-expanding uses the cache");
  assert.ok(nodeByPath(treeHost, "/docs/nested"));
  buttonByClass(nodeByPath(treeHost, "/docs"), "fb-tree-toggle").handlers.click();
  await settle();

  // Keyboard: roving tabindex and arrows.
  assert.equal(focusedTreePath(treeHost), "/");
  tree.handlers.keydown({ key: "ArrowDown", preventDefault() {} });
  assert.equal(focusedTreePath(treeHost), "/docs");
  tree.handlers.keydown({ key: "ArrowDown", preventDefault() {} });
  assert.equal(focusedTreePath(treeHost), "/a");
  tree.handlers.keydown({ key: "ArrowDown", preventDefault() {} });
  assert.equal(focusedTreePath(treeHost), "/big");
  tree.handlers.keydown({ key: "ArrowUp", preventDefault() {} });
  assert.equal(focusedTreePath(treeHost), "/a");
  tree.handlers.keydown({ key: "ArrowLeft", preventDefault() {} });
  assert.equal(focusedTreePath(treeHost), "/", "Left on a collapsed node moves to the parent");
  tree.handlers.keydown({ key: "ArrowDown", preventDefault() {} });
  tree.handlers.keydown({ key: "Enter", preventDefault() {} });
  await settle();
  assert.equal(treeBrowser.path, "/docs", "Enter opens the focused folder");
  assert.equal(location.pathname, "/default/files/docs");
  assert.match(treeHost.textContent, /old\.txt/);
  assert.equal(nodeByPath(treeHost, "/docs").attributes["aria-selected"], "true", "selecting highlights the path");
  treeBrowser.destroy();

  // Ancestors auto-expand and highlight; "load more" pages a large folder.
  session.clear();
  location.pathname = "/default/files";
  location.search = "";
  const deepHost = new Element("div");
  const deepBrowser = Clio.FileBrowser.mount(deepHost, { path: "/" });
  await deepBrowser.ready;
  await deepBrowser.navigate("/a/b");
  await settle();
  assert.equal(deepBrowser.path, "/a/b");
  assert.equal(location.pathname, "/default/files/a/b");
  assert.equal(nodeByPath(deepHost, "/").attributes["aria-expanded"], "true");
  assert.equal(nodeByPath(deepHost, "/a").attributes["aria-expanded"], "true", "the ancestor is expanded");
  assert.equal(nodeByPath(deepHost, "/a/b").attributes["aria-selected"], "true", "the current path is highlighted");
  await deepBrowser.navigate("/");
  await settle();
  buttonByClass(nodeByPath(deepHost, "/big"), "fb-tree-toggle").handlers.click();
  await settle();
  const moreRow = collectByTag(deepHost, "div").find((el) => el.className && el.className.includes("fb-tree-more"));
  assert.ok(moreRow, "a load more node appears when page.total exceeds the loaded children");
  buttonByClass(moreRow, "fb-tree-more-button").handlers.click();
  await settle();
  assert.ok(nodeByPath(deepHost, "/big/one"), "the first page is loaded");
  assert.ok(nodeByPath(deepHost, "/big/two"), "load more fetches the next page");
  assert.equal(requests.filter((r) => r.url.includes("path=%2Fbig") && r.url.includes("offset=1")).length, 1, "load more requests the next offset");
  deepBrowser.destroy();

  // Staged upload: staging never uploads, sizes and validation are shown.
  session.clear();
  location.pathname = "/default/files";
  location.search = "";
  const stageHost = new Element("div");
  const stageBrowser = Clio.FileBrowser.mount(stageHost, { path: "/" });
  await stageBrowser.ready;
  const stagingRow = (name) => collectByTag(stageHost, "div").find((el) => el.className === "fb-staging-row" && el.children.some((child) => child.className === "fb-staging-name" && child.textContent === name));
  const putCount = () => requests.filter((r) => r.options.method === "PUT").length;
  const before = putCount();
  const stagedCount = stageBrowser.stage([
    { name: "a.txt", size: 2048, type: "text/plain" },
    { name: "big.bin", size: 16 * 1024 * 1024 + 1, type: "application/octet-stream" },
    { name: "/evil/../ok.txt", size: 10, type: "text/plain" }
  ]);
  assert.equal(stagedCount, 2, "an oversized file is rejected");
  assert.equal(putCount(), before, "staging never uploads");
  assert.equal(stageBrowser.staged.length, 2);
  assert.equal(stageBrowser.staged[0].target, "/a.txt");
  assert.equal(stageBrowser.staged[1].name, "ok.txt", "path separators are stripped from names");
  assert.match(stageHost.textContent, /2\.0 KB/, "the tray shows human-readable sizes");
  assert.match(stageHost.textContent, /not staged/, "the rejection is explained");
  assert.equal(buttonByClass(stageHost, "fb-staging-upload").textContent, "Upload 2 files");
  buttonByClass(stagingRow("a.txt"), "fb-staging-remove").handlers.click();
  assert.equal(stageBrowser.staged.length, 1, "remove drops a staged row");
  assert.equal(stagingRow("a.txt"), undefined);
  stageBrowser.stage([{ name: "b.txt", size: 1, type: "text/plain" }]);
  buttonByClass(stageHost, "fb-staging-clear").handlers.click();
  assert.equal(stageBrowser.staged.length, 0, "Clear empties the tray");
  assert.equal(collectByTag(stageHost, "section").find((section) => section.className === "fb-staging").hidden, true, "the tray hides when empty");

  // Upload sends one PUT per staged file, with its body and content type.
  stageBrowser.clearStaging();
  stageBrowser.stage([
    { name: "one.txt", size: 3, type: "text/plain", body: "one" },
    { name: "two.md", size: 5, type: "text/markdown", body: "# two" }
  ]);
  const listingBefore = requests.filter((r) => r.url.endsWith("/files?path=%2F") && (r.options.method || "GET") === "GET").length;
  buttonByClass(stageHost, "fb-staging-upload").handlers.click();
  await settle();
  const uploadPuts = requests.filter((r) => r.options.method === "PUT" && (r.url.includes("path=%2Fone.txt") || r.url.includes("path=%2Ftwo.md")));
  assert.equal(uploadPuts.length, 2, "one PUT per staged file");
  assert.equal(uploadPuts[0].options.body.body, "one");
  assert.equal(uploadPuts[0].options.headers["Content-Type"], "text/plain");
  assert.equal(uploadPuts[1].options.body.body, "# two");
  assert.equal(uploadPuts[1].options.headers["Content-Type"], "text/markdown");
  assert.equal(stageBrowser.staged.length, 0, "successful rows are removed");
  assert.ok(requests.filter((r) => r.url.endsWith("/files?path=%2F") && (r.options.method || "GET") === "GET").length > listingBefore, "the listing refreshes after upload");

  // A per-file failure is shown and retained while the others succeed.
  stageBrowser.clearStaging();
  stageBrowser.stage([
    { name: "good.txt", size: 4, type: "text/plain" },
    { name: "bad.txt", size: 4, type: "text/plain" }
  ]);
  buttonByClass(stageHost, "fb-staging-upload").handlers.click();
  await settle();
  assert.equal(stageBrowser.staged.length, 1, "the failed row is retained");
  assert.equal(stageBrowser.staged[0].name, "bad.txt");
  assert.equal(stageBrowser.staged[0].status, "error");
  assert.match(stageHost.textContent, /failed/, "the failure is surfaced");
  assert.ok(requests.filter((r) => r.options.method === "PUT" && r.url.includes("path=%2Fgood.txt")).length > 0, "the good file still uploads");

  // The overwrite confirm is invoked for an existing target and can abort.
  stageBrowser.clearStaging();
  await stageBrowser.refresh();
  stageBrowser.stage([{ name: "note.txt", size: 3, type: "text/plain" }]);
  assert.equal(stageBrowser.staged[0].replaces, true, "an existing name is marked will replace");
  assert.match(stageHost.textContent, /will replace/);
  const confirmMessages = [];
  context.confirm = (message) => { confirmMessages.push(String(message)); return false; };
  const putsBeforeDecline = putCount();
  buttonByClass(stageHost, "fb-staging-upload").handlers.click();
  await settle();
  assert.equal(confirmMessages.length, 1, "the overwrite confirm is invoked");
  assert.equal(putCount(), putsBeforeDecline, "declining aborts the upload");
  assert.equal(stageBrowser.staged.length, 1, "the staged row survives a decline");
  context.confirm = (message) => { confirmMessages.push(String(message)); return true; };
  buttonByClass(stageHost, "fb-staging-upload").handlers.click();
  await settle();
  assert.ok(putCount() > putsBeforeDecline, "confirming uploads the replacement");
  delete context.confirm;
  stageBrowser.destroy();

  // Expansion state is persisted per project in sessionStorage.
  session.clear();
  location.pathname = "/default/files";
  location.search = "";
  const persistHost = new Element("div");
  const persistBrowser = Clio.FileBrowser.mount(persistHost, { path: "/" });
  await persistBrowser.ready;
  buttonByClass(nodeByPath(persistHost, "/docs"), "fb-tree-toggle").handlers.click();
  await settle();
  const saved = JSON.parse(session.get("clio-filebrowser-tree:default") || "[]");
  assert.ok(saved.includes("/docs"), "expansion state is persisted in sessionStorage");
  persistBrowser.destroy();
  session.clear();

  // --- FileBrowser context menu: one uniform list + right-click menu ---
  const fbTable = (host) => collectByTag(host, "table").find((table) => table.className === "fb-table");
  const fbRows = (host) => {
    const table = fbTable(host);
    return table ? collectByTag(table, "tr").filter((row) => row.parentNode && row.parentNode.tagName === "tbody") : [];
  };
  const fbRow = (host, path) => fbRows(host).find((row) => row.attributes["data-path"] === path);
  const fbIcon = (host, path) => collectByTag(fbRow(host, path), "span").find((span) => span.className === "fb-row-icon").textContent;
  const menuOf = (host) => collectByTag(host, "div").find((el) => el.className === "fb-menu");
  const menuLabels = (menu) => collectByTag(menu, "button").map((button) => button.textContent);
  const menuItem = (menu, label) => collectByTag(menu, "button").find((button) => button.textContent === label);

  location.pathname = "/default/files";
  location.search = "";
  const ctxHost = new Element("div");
  const ctxBrowser = Clio.FileBrowser.mount(ctxHost, { path: "/" });
  await ctxBrowser.ready;

  // One uniform table: no separate .fb-dirs block, directories before files.
  assert.ok(fbTable(ctxHost), "the right pane renders a single .fb-table");
  assert.equal(collectByTag(ctxHost, "section").filter((section) => section.className === "fb-dirs").length, 0, "there is no separate .fb-dirs block");
  assert.deepEqual(fbRows(ctxHost).map((row) => row.attributes["data-path"]), ["/a", "/big", "/docs", "/note.txt"], "directories come first, each group sorted by name");
  assert.equal(fbIcon(ctxHost, "/docs"), "📁", "directories use the folder icon");
  assert.equal(fbIcon(ctxHost, "/note.txt"), "📄", "files use the document icon");
  assert.equal(fbRow(ctxHost, "/docs").children[1].textContent, "–", "directories show a dash size");
  assert.equal(fbRow(ctxHost, "/note.txt").children[1].textContent, "3 B", "files show a human-readable size");
  assert.equal(collectByTag(ctxHost, "button").filter((button) => button.className === "fb-danger" || button.textContent === "Rename" || button.textContent === "Delete").length, 0, "the old inline Rename/Delete buttons are gone");
  assert.equal(collectByTag(ctxHost, "button").filter((button) => button.className === "fb-row-menu").length, 0, "the kebab column is gone");
  const sortButton = (label) => collectByTag(ctxHost, "button").find((button) => button.className === "fb-sort" && button.textContent.replace(/[ ▲▼]+$/, "") === label);
  assert.deepEqual(
    collectByTag(ctxHost, "button").filter((button) => button.className === "fb-sort").map((button) => button.textContent.replace(/[ ▲▼]+$/, "")),
    ["Name", "Size", "Created", "Modified"],
    "the columns are Name/Size/Created/Modified"
  );
  assert.equal(fbRow(ctxHost, "/note.txt").children[2].textContent, "2026-01-02 03:04", "Created shows the created_at timestamp");
  assert.equal(fbRow(ctxHost, "/note.txt").children[3].textContent, "2026-02-03 04:05", "Modified shows the updated_at timestamp");
  assert.equal(fbRow(ctxHost, "/docs").children[2].textContent, "–", "directories show a dash for Created");

  // Right-click a file row opens the full menu at the pointer.
  fbRow(ctxHost, "/note.txt").handlers.contextmenu({ preventDefault() {}, clientX: 24, clientY: 30 });
  let menu = menuOf(ctxHost);
  assert.ok(menu, "right-click opens a context menu");
  assert.equal(menu.attributes.role, "menu");
  assert.deepEqual(menuLabels(menu), ["Open", "Download", "Rename", "Delete"], "the file menu has Open/Download/Rename/Delete");
  assert.ok(collectByTag(menu, "button").every((button) => button.attributes.role === "menuitem"), "every menu entry is a menuitem");
  assert.equal(menu.style.position, "fixed", "the menu floats");
  assert.equal(menu.style.left, "24px");
  assert.equal(menu.style.top, "30px");

  // Download uses the stable ID URL and closes the menu.
  const openedBefore = opened.length;
  menuItem(menu, "Download").handlers.click({ preventDefault() {}, stopPropagation() {} });
  assert.equal(opened.at(-1), "/default/files/id/file-2", "Download opens the stable ID URL");
  assert.ok(opened.length > openedBefore, "Download opens a new tab/URL");
  assert.equal(menuOf(ctxHost), undefined, "acting on an item closes the menu");

  // Rename prompts and issues the move request.
  context.prompt = () => "renamed.txt";
  const movesBefore = requests.filter((request) => request.url.endsWith("/files/move")).length;
  fbRow(ctxHost, "/note.txt").handlers.contextmenu({ preventDefault() {}, clientX: 0, clientY: 0 });
  menuItem(menuOf(ctxHost), "Rename").handlers.click({ preventDefault() {}, stopPropagation() {} });
  await settle();
  const renameRequest = requests.filter((request) => request.url.endsWith("/files/move")).at(-1);
  assert.ok(requests.filter((request) => request.url.endsWith("/files/move")).length > movesBefore, "Rename issues a move");
  assert.deepEqual(JSON.parse(renameRequest.options.body), { from: "/note.txt", to: "/renamed.txt" });
  delete context.prompt;

  // Delete confirms and issues the delete request.
  context.confirm = () => true;
  const deletesBefore = requests.filter((request) => request.options.method === "DELETE").length;
  fbRow(ctxHost, "/note.txt").handlers.contextmenu({ preventDefault() {}, clientX: 0, clientY: 0 });
  menuItem(menuOf(ctxHost), "Delete").handlers.click({ preventDefault() {}, stopPropagation() {} });
  await settle();
  const contextDeleteRequest = requests.filter((request) => request.options.method === "DELETE").at(-1);
  assert.ok(requests.filter((request) => request.options.method === "DELETE").length > deletesBefore, "Delete issues a delete");
  assert.match(contextDeleteRequest.url, /\/files\?path=%2Fnote\.txt$/);
  delete context.confirm;

  // A directory menu omits Download.
  fbRow(ctxHost, "/docs").handlers.contextmenu({ preventDefault() {}, clientX: 0, clientY: 0 });
  assert.deepEqual(menuLabels(menuOf(ctxHost)), ["Open", "Rename", "Delete"], "the directory menu omits Download");
  context.document.dispatchEvent("mousedown", { target: new Element("div") });
  assert.equal(menuOf(ctxHost), undefined, "an outside mousedown closes the menu");

  // Sorting: clicking the Name header toggles direction, folders still first.
  sortButton("Name").handlers.click({ preventDefault() {} });
  assert.deepEqual(fbRows(ctxHost).map((row) => row.attributes["data-path"]), ["/docs", "/big", "/a", "/note.txt"], "clicking Name sorts descending within each group");
  sortButton("Name").handlers.click({ preventDefault() {} });
  assert.deepEqual(fbRows(ctxHost).map((row) => row.attributes["data-path"]), ["/a", "/big", "/docs", "/note.txt"], "clicking Name again restores ascending");

  // Right-click opens the menu; Escape closes it and returns focus to the row.
  fbRow(ctxHost, "/docs").handlers.contextmenu({ preventDefault() {}, clientX: 5, clientY: 6 });
  menu = menuOf(ctxHost);
  assert.ok(menu, "right-click opens the context menu for a directory");
  assert.deepEqual(menuLabels(menu), ["Open", "Rename", "Delete"]);
  assert.equal(context.document.activeElement, collectByTag(menu, "button")[0], "focus moves into the menu");
  menu.handlers.keydown({ key: "ArrowDown", preventDefault() {} });
  assert.equal(context.document.activeElement, collectByTag(menu, "button")[1], "ArrowDown moves to the next item");
  menu.handlers.keydown({ key: "Escape", preventDefault() {}, stopPropagation() {} });
  assert.equal(menuOf(ctxHost), undefined, "Escape closes the menu");
  assert.equal(context.document.activeElement, fbRow(ctxHost, "/docs"), "focus returns to the row after closing");

  // Keyboard open for the selected row: Shift+F10 then ContextMenu key.
  fbRow(ctxHost, "/note.txt").handlers.click({});
  const listEl = collectByTag(ctxHost, "div").find((el) => el.className === "fb-list");
  listEl.handlers.keydown({ key: "F10", shiftKey: true, preventDefault() {} });
  assert.deepEqual(menuLabels(menuOf(ctxHost)), ["Open", "Download", "Rename", "Delete"], "Shift+F10 opens the menu for the selected row");
  context.document.dispatchEvent("keydown", { key: "Escape" });
  assert.equal(menuOf(ctxHost), undefined, "document Escape closes the menu");
  listEl.handlers.keydown({ key: "ContextMenu", shiftKey: false, preventDefault() {} });
  assert.ok(menuOf(ctxHost), "the ContextMenu key opens the menu");
  context.document.dispatchEvent("scroll", {});
  assert.equal(menuOf(ctxHost), undefined, "scrolling closes the menu");

  ctxBrowser.destroy();

  // Projects manager over the instance-level projects API.
  location.pathname = "/default/";
  location.search = "";
  const projectsHost = new Element("div");
  const manager = Clio.Projects.mount(projectsHost, { client: new Clio() });
  await manager.ready;
  assert.match(projectsHost.textContent, /Default/);
  assert.match(projectsHost.textContent, /Bills/);
  const listRequest = requests.filter((request) => request.url.endsWith("/api/v1/projects") && (request.options.method || "GET") === "GET").at(-1);
  assert.ok(listRequest, "the project list comes from GET /api/v1/projects");
  const projectLinks = collectByTag(projectsHost, "a").map((link) => link.href);
  assert.ok(projectLinks.includes("/default/"), "default links to /default/");
  assert.ok(projectLinks.includes("/bills/"), "a project links to /{name}/");
  const defaultRow = collectByTag(projectsHost, "li").find((item) => item.attributes["data-project"] === "default");
  assert.ok(defaultRow, "default is listed");
  assert.equal(collectByTag(defaultRow, "button").filter((button) => button.className === "projects-danger").length, 0, "default has no delete action");
  assert.equal(collectByTag(projectsHost, "button").filter((button) => button.className === "projects-danger").length, 1, "only a non-default project offers delete");

  await manager.create({ name: "bills", label: "Bills Two", order: 2 });
  const createProjectRequest = requests.filter((request) => request.url.endsWith("/api/v1/projects") && request.options.method === "POST").at(-1);
  assert.ok(createProjectRequest, "create uses POST /api/v1/projects");
  assert.deepEqual(JSON.parse(createProjectRequest.options.body), { name: "bills", label: "Bills Two", order: 2 });
  assert.match(projectsHost.textContent, /Created bills/);

  await manager.remove("busy");
  const deleteProjectRequest = requests.filter((request) => request.options.method === "DELETE" && request.url.endsWith("/api/v1/projects/busy")).at(-1);
  assert.ok(deleteProjectRequest, "delete uses DELETE /api/v1/projects/{name}");
  assert.match(projectsHost.textContent, /not empty/, "a 409 delete error is surfaced");
  manager.destroy();

  // The data browser toolbar carries a project switcher that preserves the query.
  location.pathname = "/default/data";
  location.search = "?group=pool&table=measurements&page=2";
  const dataSwitchHost = new Element("div");
  const dataSwitchBrowser = Clio.DataBrowser.mount(dataSwitchHost, { pageSize: 2 });
  await dataSwitchBrowser.ready;
  const dataProjectSelect = collectByTag(dataSwitchHost, "select")[0];
  assert.ok(dataProjectSelect, "the data browser toolbar has a project switcher");
  dataProjectSelect.value = "bills";
  dataProjectSelect.handlers.change();
  assert.equal(navigation.at(-1), "/bills/data?group=pool&table=measurements&page=2", "the switcher preserves the data-browser query");
  dataSwitchBrowser.destroy();

  // The file browser toolbar carries a project switcher that keeps the sub-path.
  location.pathname = "/default/files";
  location.search = "";
  const fileSwitchHost = new Element("div");
  const fileSwitchBrowser = Clio.FileBrowser.mount(fileSwitchHost, { path: "/" });
  await fileSwitchBrowser.ready;
  await fileSwitchBrowser.navigate("/docs");
  const fileProjectSelect = collectByTag(fileSwitchHost, "select")[0];
  assert.ok(fileProjectSelect, "the file browser toolbar has a project switcher");
  fileProjectSelect.value = "bills";
  fileProjectSelect.handlers.change();
  assert.equal(navigation.at(-1), "/bills/files/docs", "the switcher keeps the file sub-path");
  fileSwitchBrowser.destroy();

  // A non-default project exposes no cross-project switcher in either browser;
  // the only affordance is a single Home link to bare "/". The project name is
  // still resolvable from the URL, so the mount targets bills.
  location.pathname = "/bills/data";
  location.search = "?group=pool&table=measurements";
  const nonDefaultDataHost = new Element("div");
  const nonDefaultData = Clio.DataBrowser.mount(nonDefaultDataHost, { pageSize: 2 });
  await nonDefaultData.ready;
  const nonDefaultDataSelects = collectByTag(nonDefaultDataHost, "select").filter((select) => select.attributes["aria-label"] === "Project");
  assert.equal(nonDefaultDataSelects.length, 0, "a non-default data browser has no project switcher");
  const nonDefaultDataHome = collectByTag(nonDefaultDataHost, "a").find((link) => link.className === "project-home");
  assert.ok(nonDefaultDataHome, "a non-default data browser offers a Home link");
  assert.equal(nonDefaultDataHome.href, "/", "the Home link points at bare /");
  nonDefaultData.destroy();

  location.pathname = "/bills/files";
  location.search = "";
  const nonDefaultFileHost = new Element("div");
  const nonDefaultFile = Clio.FileBrowser.mount(nonDefaultFileHost, { path: "/" });
  await nonDefaultFile.ready;
  await nonDefaultFile.navigate("/docs");
  const nonDefaultFileSelects = collectByTag(nonDefaultFileHost, "select").filter((select) => select.attributes["aria-label"] === "Project");
  assert.equal(nonDefaultFileSelects.length, 0, "a non-default file browser has no project switcher");
  const nonDefaultFileHome = collectByTag(nonDefaultFileHost, "a").find((link) => link.className === "project-home");
  assert.ok(nonDefaultFileHome, "a non-default file browser offers a Home link");
  assert.equal(nonDefaultFileHome.href, "/", "the Home link points at bare /");
  nonDefaultFile.destroy();

  // Clio.Projects.mount is robust outside the default project: it offers only a
  // Home link to "/" and never lists or links to another project.
  location.pathname = "/bills/";
  location.search = "";
  const nonDefaultManagerHost = new Element("div");
  const nonDefaultManager = Clio.Projects.mount(nonDefaultManagerHost, { client: new Clio() });
  await nonDefaultManager.ready;
  assert.deepEqual(collectByTag(nonDefaultManagerHost, "a").map((link) => link.href), ["/"], "a non-default projects mount links only to bare /");
  assert.match(nonDefaultManagerHost.textContent, /Home/);
  assert.equal(collectByTag(nonDefaultManagerHost, "select").length, 0, "a non-default projects mount has no project list or form controls");
  nonDefaultManager.destroy();

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

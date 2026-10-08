(function (root) {
  "use strict";

  const apiVersion = "v1";
  const libraryVersion = "1.3.0";

  class ClioError extends Error {
    constructor(status, code, message, body) {
      super(message || `Clio request failed with HTTP ${status}`);
      this.name = "ClioError";
      this.status = status;
      this.code = code || "http_error";
      this.body = body;
    }
  }

  function queryString(options) {
    const params = new URLSearchParams();
    for (const [key, value] of Object.entries(options || {})) {
      if (key === "filter" || key === "signal" || value == null) continue;
      if (Array.isArray(value)) {
        for (const item of value) params.append(key, String(item));
      } else if (typeof value === "object") {
        params.set(key, JSON.stringify(value));
      } else {
        params.set(key, String(value));
      }
    }
    const filters = options && options.filter;
    if (filters != null) {
      for (const [field, value] of Object.entries(filters)) {
        const key = field.startsWith("filter.") ? field : `filter.${field}`;
        if (Array.isArray(value)) {
          for (const item of value) params.append(key, String(item));
        } else if (value != null) {
          params.append(key, String(value));
        }
      }
    }
    return params.toString();
  }

  class Clio {
    constructor(options) {
      options = options || {};
      const origin = root.location && root.location.origin;
      this.baseUrl = String(options.baseUrl || origin || "").replace(/\/+$/, "");
      this.project = String(options.project || "default");
      this.apiUrl = `${this.baseUrl}/api/${apiVersion}/${encodeURIComponent(this.project)}`;
      this._fetch = options.fetch || root.fetch;
      this._headers = options.headers || {};
      this._credentials = options.credentials || "same-origin";
      if (typeof this._fetch !== "function") {
        throw new TypeError("Clio requires the browser fetch() API");
      }
    }

    async _request(url, options) {
      options = options || {};
      const headers = Object.assign({}, this._headers, options.headers || {});
      const init = Object.assign({ credentials: this._credentials }, options, { headers });
      delete init.json;
      if (Object.prototype.hasOwnProperty.call(options, "json")) {
        headers["Content-Type"] = "application/json";
        init.body = JSON.stringify(options.json);
      }
      const response = await this._fetch.call(root, url, init);
      if (response.status === 204) return null;
      const text = await response.text();
      let body = null;
      if (text) {
        try {
          body = JSON.parse(text);
        } catch (_) {
          body = text;
        }
      }
      if (!response.ok) {
        const detail = body && typeof body === "object" ? body : {};
        throw new ClioError(response.status, detail.error, detail.message, body);
      }
      return body;
    }

    _get(path, options) {
      const query = queryString(options);
      const signal = options && options.signal;
      return this._request(`${this.apiUrl}${path}${query ? `?${query}` : ""}`, { signal });
    }

    _mutate(method, path, value, options) {
      return this._request(`${this.apiUrl}${path}`, {
        method,
        json: value,
        signal: options && options.signal
      });
    }

    metadata(options) { return this._get("/data/metadata", options); }
    groups(options) { return this._get("/data/groups", options); }
    createGroup(value, options) { return this._mutate("POST", "/data/groups", value, options); }

    group(name) {
      const client = this;
      const groupName = name;
      const base = `/data/groups/${encodeURIComponent(name)}`;
      return {
        get(options) { return client._get(base, options); },
        tables(options) { return client._get(`${base}/tables`, options); },
        createTable(value, options) { return client._mutate("POST", `${base}/tables`, value, options); },
        table(tableName) { return client.table(groupName, tableName); }
      };
    }

    table(group, name) {
      if (!group || !name) throw new TypeError("clio.table() requires a group and table name");
      const client = this;
      const base = `/data/groups/${encodeURIComponent(group)}/tables/${encodeURIComponent(name)}`;
      const recordsPath = `${base}/records`;
      const table = {
        metadata(options) { return client._get(base, options); },
        query(options) { return client._get(recordsPath, options); },
        get(id, options) { return client._get(`${recordsPath}/${encodeURIComponent(id)}`, options); },
        create(value, options) { return client._mutate("POST", recordsPath, value, options); },
        update(id, value, options) { return client._mutate("PATCH", `${recordsPath}/${encodeURIComponent(id)}`, value, options); },
        delete(id, options) { return client._request(`${client.apiUrl}${recordsPath}/${encodeURIComponent(id)}`, { method: "DELETE", signal: options && options.signal }); },
        async list(options) {
          const result = await this.query(options);
          return result && Array.isArray(result.data) ? result.data : [];
        },
        async first(options) {
          const result = await this.query(Object.assign({}, options, { limit: 1, offset: 0 }));
          return result && Array.isArray(result.data) ? (result.data[0] || null) : null;
        },
        async count(options) {
          const result = await this.query(Object.assign({}, options, { limit: 1, offset: 0 }));
          return result && result.page ? result.page.total : 0;
        },
        distinct(field, options) { return this.query(Object.assign({}, options, { distinct: field })); },
        aggregate(spec, options) { return this.query(Object.assign({}, options, { aggregate: spec })); },
        records(options) { return iterateRecords(client, recordsPath, options); }
      };
      return table;
    }

    // directory returns the files-partition directory or entry node at a path.
    directory(path, options) {
      return this._get("/files", Object.assign({}, options, { path }));
    }

    // page returns a page's metadata and its original source bytes, composed
    // from the files partition: the entry is resolved by path, then its stored
    // bytes are read from the stable content URL (section 66.7).
    async page(path, options) {
      const entry = await this._get("/files", Object.assign({}, options, { path }));
      if (entry && entry.id && entry.kind !== "directory") {
        const content = await this.fileContent(entry.id, options);
        return Object.assign({}, entry, { content });
      }
      return entry;
    }

    // fileContent returns the raw stored bytes (as text) of an entry by ID.
    fileContent(id, options) {
      return this._request(`${this.apiUrl}/files/${encodeURIComponent(id)}/content`, { signal: options && options.signal });
    }

    createDirectory(path, options) {
      return this._mutate("POST", "/files/directories", { path }, options);
    }

    deleteDirectory(path, options) {
      return this.deleteFile(path, options);
    }

    // publishPage creates or replaces a page with PUT /files?path=, sending the
    // source as raw bytes (section 66.7).
    publishPage(value, options) {
      value = value || {};
      const path = value.path;
      const contentType = value.content_type || (String(path).endsWith(".html") ? "text/html" : "text/markdown");
      const query = queryString({ path });
      return this._request(`${this.apiUrl}/files?${query}`, {
        method: "PUT",
        body: value.content == null ? "" : String(value.content),
        headers: { "Content-Type": contentType },
        signal: options && options.signal
      });
    }

    deletePage(path, options) {
      return this.deleteFile(path, options);
    }

    // deleteFile deletes a page, file or directory subtree by path.
    deleteFile(path, options) {
      const query = queryString({ path });
      return this._request(`${this.apiUrl}/files?${query}`, { method: "DELETE", signal: options && options.signal });
    }

    // files lists the flat content catalog (section 64.4).
    files(options) {
      return this._get("/files", options);
    }

    // search queries the project text index (section 64.7). The result snippet is
    // plain text with private sentinels; escape it before applying markup.
    search(query, options) {
      return this._get("/search", Object.assign({}, options, { q: query }));
    }

    // getFile reads one content entry by its stable ID (section 64.4).
    getFile(id, options) {
      return this._get(`/files/${encodeURIComponent(id)}`, options);
    }

    // moveFile renames or moves a page, file or directory, preserving IDs
    // (section 64.4).
    moveFile(from, to, options) {
      return this._mutate("POST", "/files/move", { from, to }, options);
    }

    // copyFile copies an entry, assigning new IDs (section 64.4).
    copyFile(from, to, options) {
      return this._mutate("POST", "/files/copy", { from, to }, options);
    }

    // putFile creates or replaces raw bytes at a path (section 64.4), honoring an
    // explicit content type. `body` may be a string, Blob or File.
    putFile(path, body, options) {
      options = options || {};
      const query = queryString({ path });
      const headers = Object.assign({}, options.headers || {});
      if (options.contentType) headers["Content-Type"] = options.contentType;
      return this._request(`${this.apiUrl}/files?${query}`, {
        method: "PUT",
        body,
        headers,
        signal: options.signal
      });
    }

    // projects lists every project from the instance-level projects API
    // (section 65.5). It is deliberately not project-prefixed.
    projects(options) {
      return this._request(`${this.baseUrl}/api/${apiVersion}/projects`, {
        signal: options && options.signal
      });
    }

    // createProject creates a project from {name, label, description, order}
    // (section 65.5). A duplicate is 409 and a reserved/default name is 422.
    createProject(value, options) {
      return this._request(`${this.baseUrl}/api/${apiVersion}/projects`, {
        method: "POST",
        json: value,
        signal: options && options.signal
      });
    }

    // deleteProject removes an empty project (204). `default` is never
    // deletable (422) and a non-empty project returns 409 (section 65.5).
    deleteProject(name, options) {
      return this._request(`${this.baseUrl}/api/${apiVersion}/projects/${encodeURIComponent(name)}`, {
        method: "DELETE",
        signal: options && options.signal
      });
    }
  }

  async function* iterateRecords(client, recordsPath, options) {
    const query = Object.assign({ limit: 100, offset: 0 }, options || {});
    let offset = Number(query.offset) || 0;
    const pageLimit = Math.min(1000, Math.max(1, Number(query.limit) || 100));
    query.limit = pageLimit;
    while (true) {
      query.offset = offset;
      const result = await client._get(recordsPath, query);
      const rows = result && Array.isArray(result.data) ? result.data : [];
      const page = result && result.page ? result.page : {};
      for (const row of rows) yield row;
      const count = Number(page.count);
      const total = Number(page.total);
      const advancedBy = Number.isFinite(count) && count > 0 ? count : rows.length;
      if (advancedBy === 0 || rows.length === 0) return;
      offset += advancedBy;
      if (Number.isFinite(total) && offset >= total) return;
      if (!Number.isFinite(total) && rows.length < pageLimit) return;
    }
  }

  Object.defineProperties(Clio, {
    version: { value: libraryVersion, enumerable: true },
    apiVersion: { value: apiVersion, enumerable: true },
    Error: { value: ClioError, enumerable: true },
    Markdown: {
      enumerable: true,
      get() {
        if (!root.ClioMarkdown || typeof root.ClioMarkdown.render !== "function") {
          throw new Error("Load /assets/clio-markdown.js to use Clio.Markdown.render()");
        }
        return root.ClioMarkdown;
      }
    }
  });

  // browserProject resolves the project a mounted DataBrowser belongs to. The
  // data browser lives at /{project}/data, so the project is the first path
  // segment.
  function browserProject(options) {
    if (options && options.project) return String(options.project);
    const parts = ((root.location && root.location.pathname) || "").split("/").filter(Boolean);
    if (parts.length > 1 && parts[1] === "data") return parts[0];
    return "default";
  }

  // currentProjectFromLocation resolves the active project from the first path
  // segment of the human URL, defaulting to `default`.
  function currentProjectFromLocation() {
    const parts = ((root.location && root.location.pathname) || "").split("/").filter(Boolean);
    return parts.length >= 1 ? parts[0] : "default";
  }

  // projectSwitchTarget builds the same human sub-path under another project,
  // preserving any current query string.
  function projectSwitchTarget(project, path) {
    const search = (root.location && root.location.search) || "";
    return `/${encodeURIComponent(project)}${path}${search}`;
  }

  // switchProject navigates to the same sub-path under another project. It
  // prefers a full navigation so the mounted client re-targets the project and
  // falls back to history.pushState for test doubles or embedded views.
  function switchProject(project, path) {
    if (!project) return false;
    const target = projectSwitchTarget(project, path);
    if (root.location && typeof root.location.assign === "function") {
      root.location.assign(target);
      return true;
    }
    if (root.history && typeof root.history.pushState === "function") {
      root.history.pushState({}, "", target);
      return true;
    }
    if (root.location) {
      root.location.href = target;
      return true;
    }
    return false;
  }

  // buildProjectSwitcher creates a compact project <select> for a toolbar. It
  // starts with the current project and is filled from the API asynchronously.
  function buildProjectSwitcher(doc, currentProject, onChange) {
    const label = doc.createElement("label");
    label.className = "project-switcher";
    if (doc.createTextNode) label.appendChild(doc.createTextNode("Project: "));
    const select = doc.createElement("select");
    select.setAttribute("aria-label", "Project");
    const option = doc.createElement("option");
    option.value = currentProject;
    option.textContent = currentProject;
    select.appendChild(option);
    select.addEventListener("change", () => onChange(select.value));
    label.appendChild(select);
    return { label, select };
  }

  // fillProjectSwitcher populates a project <select> from the projects API.
  // Every label is assigned as text, so server data is never rendered as HTML.
  async function fillProjectSwitcher(doc, select, client, currentProject) {
    try {
      const projects = await client.projects();
      if (!Array.isArray(projects)) return;
      select.replaceChildren();
      for (const item of projects) {
        const option = doc.createElement("option");
        option.value = String(item.name);
        option.textContent = item.label ? String(item.label) : String(item.name);
        option.selected = String(item.name) === currentProject;
        select.appendChild(option);
      }
      select.value = currentProject;
    } catch (_) {
      // Keep the current project when the project list cannot be loaded.
    }
  }

  // appendProjectNav appends the default-project-only project switcher to a
  // toolbar and returns it. The switcher is the only cross-project affordance,
  // so it exists only in the default project; a non-default project gets a
  // single Home link to bare "/" instead, which the server redirects to the
  // default project. It never lists the other projects (section 66.1).
  function appendProjectNav(doc, toolbar, currentProject, onChange) {
    if (currentProject !== "default") {
      const home = doc.createElement("a");
      home.className = "project-home";
      home.href = "/";
      home.textContent = "Home";
      toolbar.appendChild(home);
      return null;
    }
    const switcher = buildProjectSwitcher(doc, currentProject, onChange);
    toolbar.appendChild(switcher.label);
    return switcher;
  }

  const DataBrowser = {
    mount(target, options) {
      options = options || {};
      const doc = root.document;
      if (!doc) throw new TypeError("Clio.DataBrowser.mount() requires a browser element");
      const host = typeof target === "string" ? doc.querySelector(target) : target;
      if (!host) throw new TypeError("Clio.DataBrowser.mount() requires a browser element");
      const project = browserProject(options);
      const client = options.client || new Clio(Object.assign({}, options, { project }));
      const pageSize = Math.min(1000, Math.max(1, Number(options.pageSize) || 50));
      let currentGroup = "";
      let currentTable = "";
      let groups = [];
      let requestNumber = 0;
      let destroyed = false;

      const element = (tag, text, className) => {
        const value = doc.createElement(tag);
        if (text != null) value.textContent = String(text);
        if (className) value.className = className;
        return value;
      };
      const toolbar = element("div", null, "browser-toolbar");
      const projectSwitcher = appendProjectNav(doc, toolbar, project, (next) => {
        if (next && next !== project) switchProject(next, "/data");
      });
      const collectionLabel = element("label", "Collection:");
      const collectionSelect = element("select");
      collectionSelect.setAttribute("aria-label", "Collection");
      collectionLabel.appendChild(collectionSelect);
      toolbar.appendChild(collectionLabel);
      const tableViewLink = element("a", "Table view ↗", "browser-table-view");
      tableViewLink.hidden = true;
      tableViewLink.setAttribute("aria-label", "Open table management view");
      toolbar.appendChild(tableViewLink);
      const themeToggle = element("button", null, "browser-theme-toggle");
      themeToggle.type = "button";
      themeToggle.setAttribute("aria-pressed", "false");
      toolbar.appendChild(themeToggle);
      let theme;
      try {
        theme = root.localStorage.getItem("clio-data-browser-theme");
      } catch (_) {}
      if (theme !== "light" && theme !== "dark") {
        theme = root.matchMedia && root.matchMedia("(prefers-color-scheme: dark)").matches ? "dark" : "light";
      }
      const updateTheme = (persist) => {
        doc.documentElement.setAttribute("data-theme", theme);
        const nextTheme = theme === "dark" ? "light" : "dark";
        themeToggle.textContent = theme === "dark" ? "☀ Light" : "☾ Dark";
        themeToggle.setAttribute("aria-label", `Switch to ${nextTheme} theme`);
        themeToggle.setAttribute("aria-pressed", String(theme === "dark"));
        if (persist) {
          try {
            root.localStorage.setItem("clio-data-browser-theme", theme);
          } catch (_) {}
        }
      };
      updateTheme(false);
      themeToggle.addEventListener("click", () => {
        theme = theme === "dark" ? "light" : "dark";
        updateTheme(true);
      });
      const layout = element("div", null, "browser-layout");
      const sidebar = element("aside", null, "browser-tables");
      sidebar.appendChild(element("h2", "Tables"));
      const tableList = element("ul");
      sidebar.appendChild(tableList);
      const content = element("section", null, "browser-content");
      content.setAttribute("aria-label", "Table data");
      const title = element("h2", "");
      const status = element("p", "Loading…", "browser-status");
      status.setAttribute("role", "status");
      const grid = element("div", null, "browser-grid");
      const pagination = element("nav", null, "browser-pagination");
      pagination.setAttribute("aria-label", "Pages");
      content.append(title, status, grid, pagination);
      layout.append(sidebar, content);
      host.replaceChildren(toolbar, layout);

      function selectedPage() {
        const params = new URLSearchParams((root.location && root.location.search) || "");
        const value = Number(params.get("page") || 1);
        return Number.isFinite(value) && value > 0 ? Math.floor(value) : 1;
      }

      function browserURL(group, table, page) {
        const params = new URLSearchParams();
        if (group) params.set("group", group);
        if (table) params.set("table", table);
        if (page > 1) params.set("page", String(page));
        const query = params.toString();
        return `/${encodeURIComponent(project)}/data${query ? `?${query}` : ""}`;
      }

      function setURL(group, table, page, replace) {
        const url = browserURL(group, table, page);
        if (root.history && root.history[replace ? "replaceState" : "pushState"]) {
          root.history[replace ? "replaceState" : "pushState"]({}, "", url);
        }
      }

      function drawCollections() {
        collectionSelect.replaceChildren();
        for (const group of groups) {
          const option = element("option", group.label || group.name);
          option.value = group.name;
          option.selected = group.name === currentGroup;
          collectionSelect.appendChild(option);
        }
        collectionLabel.hidden = groups.length === 0;
      }

      function drawTableLinks(tables) {
        tableList.replaceChildren();
        for (const table of tables) {
          const item = element("li");
          const link = element("a", table.label || table.name);
          link.href = browserURL(currentGroup, table.name, 1);
          if (table.name === currentTable) link.setAttribute("aria-current", "page");
          link.addEventListener("click", (event) => {
            event.preventDefault();
            navigate(currentGroup, table.name, 1);
          });
          item.appendChild(link);
          tableList.appendChild(item);
        }
      }

      function valueText(value) {
        if (value == null) return "";
        if (typeof value === "object") return JSON.stringify(value);
        return String(value);
      }

      function drawGrid(fields, records) {
        grid.replaceChildren();
        const visibleFields = fields.filter((field) => field.hidden !== true);
        const table = element("table");
        const head = element("thead");
        const headingRow = element("tr");
        for (const field of visibleFields) headingRow.appendChild(element("th", field.label || field.name));
        head.appendChild(headingRow);
        table.appendChild(head);
        const body = element("tbody");
        for (const record of records) {
          const row = element("tr");
          for (const field of visibleFields) row.appendChild(element("td", valueText(record[field.name])));
          body.appendChild(row);
        }
        table.appendChild(body);
        grid.appendChild(table);
      }

      function drawPages(page, selected) {
        pagination.replaceChildren();
        const total = Math.max(0, Number(page && page.total) || 0);
        const pages = Math.ceil(total / pageSize);
        const addPage = (number, label) => {
          const link = element("a", label || number);
          link.href = browserURL(currentGroup, currentTable, number);
          if (number === selected) link.setAttribute("aria-current", "page");
          link.addEventListener("click", (event) => {
            event.preventDefault();
            navigate(currentGroup, currentTable, number);
          });
          pagination.appendChild(link);
        };
        if (selected > 1) addPage(selected - 1, "Previous");
        const start = Math.max(1, Math.min(selected - 2, pages - 4));
        const end = Math.min(pages, Math.max(5, selected + 2));
        if (start > 1) {
          addPage(1);
          if (start > 2) pagination.appendChild(element("span", "…"));
        }
        for (let number = start; number <= end; number++) addPage(number);
        if (end < pages) {
          if (end < pages - 1) pagination.appendChild(element("span", "…"));
          addPage(pages);
        }
        if (selected < pages) addPage(selected + 1, "Next");
        pagination.hidden = pages <= 1;
      }

      async function navigate(group, table, page, replaceURL) {
        const request = ++requestNumber;
        currentGroup = group || "";
        currentTable = table || "";
        page = Math.max(1, Number(page) || 1);
        title.textContent = "";
        grid.replaceChildren();
        pagination.replaceChildren();
        status.className = "browser-status";
        status.textContent = "Loading…";
        try {
          if (!groups.length) groups = await client.groups();
          if (destroyed || request !== requestNumber) return;
          drawCollections();
          if (!groups.length) {
            currentGroup = currentTable = "";
            status.textContent = "No collections are available.";
            setURL("", "", 1, replaceURL === true);
            return;
          }
          let groupItem = groups.find((item) => item.name === currentGroup);
          if (!groupItem) groupItem = groups[0];
          currentGroup = groupItem.name;
          const tables = await client.group(currentGroup).tables();
          if (destroyed || request !== requestNumber) return;
          let tableItem = tables.find((item) => item.name === currentTable);
          if (!tableItem) tableItem = tables[0];
          currentTable = tableItem ? tableItem.name : "";
          tableViewLink.hidden = !currentTable;
          if (currentTable) tableViewLink.href = `/${encodeURIComponent(project)}/data/${encodeURIComponent(currentGroup)}/${encodeURIComponent(currentTable)}`;
          drawCollections();
          drawTableLinks(tables);
          if (!tableItem) {
            title.textContent = groupItem.label || groupItem.name;
            status.textContent = "This collection has no tables.";
            setURL(currentGroup, "", 1, replaceURL === true);
            return;
          }
          const tableClient = client.table(currentGroup, currentTable);
          const [metadata, result] = await Promise.all([
            tableClient.metadata(),
            tableClient.query({ limit: pageSize, offset: (page - 1) * pageSize })
          ]);
          if (destroyed || request !== requestNumber) return;
          const paging = result && result.page ? result.page : {};
          const total = Number(paging.total) || 0;
          const lastPage = Math.max(1, Math.ceil(total / pageSize));
          if (page > lastPage) {
            navigate(currentGroup, currentTable, lastPage, true);
            return;
          }
          title.textContent = metadata.label || metadata.name;
          const fields = Array.isArray(metadata.fields) ? metadata.fields : [];
          const records = result && Array.isArray(result.data) ? result.data : [];
          drawGrid(fields, records);
          const start = records.length ? (page - 1) * pageSize + 1 : 0;
          const end = records.length ? Math.min((page - 1) * pageSize + records.length, total) : 0;
          status.textContent = `${start}–${end} of ${total} records`;
          drawPages(paging, page);
          setURL(currentGroup, currentTable, page, replaceURL === true);
        } catch (error) {
          if (destroyed || request !== requestNumber) return;
          status.className = "browser-status browser-error";
          status.textContent = error && error.message ? error.message : "Unable to load collection data.";
        }
      }

      collectionSelect.addEventListener("change", () => navigate(collectionSelect.value, currentTable, 1));
      const initialParams = new URLSearchParams((root.location && root.location.search) || "");
      const initialGroup = initialParams.get("group") || "";
      const initialTable = initialParams.get("table") || "";
      const popstate = () => {
        const params = new URLSearchParams((root.location && root.location.search) || "");
        navigate(params.get("group") || "", params.get("table") || "", selectedPage(), true);
      };
      if (root.addEventListener) root.addEventListener("popstate", popstate);
      const ready = navigate(initialGroup, initialTable, selectedPage(), true);
      const projectsReady = projectSwitcher
        ? fillProjectSwitcher(doc, projectSwitcher.select, client, project)
        : Promise.resolve();
      return {
        ready: Promise.all([ready, projectsReady]).then(() => undefined),
        refresh() {
          groups = [];
          return navigate(currentGroup, currentTable, selectedPage(), true);
        },
        destroy() {
          destroyed = true;
          if (root.removeEventListener) root.removeEventListener("popstate", popstate);
          host.replaceChildren();
        }
      };
    }
  };

  // fileBrowserProject resolves the project a mounted FileBrowser belongs to.
  // The explorer lives at /{project}/files, so the project is the first path
  // segment.
  function fileBrowserProject(options) {
    if (options && options.project) return String(options.project);
    const parts = ((root.location && root.location.pathname) || "").split("/").filter(Boolean);
    if (parts.length > 1 && parts[1] === "files") return parts[0];
    return "default";
  }

  function normalizeContentPath(value) {
    let path = String(value == null ? "/" : value).trim();
    if (path === "" || path === "/") return "/";
    if (path[0] !== "/") path = "/" + path;
    return path.replace(/\/+$/, "");
  }

  function joinContentPath(base, name) {
    const parent = normalizeContentPath(base);
    const leaf = String(name == null ? "" : name).replace(/^\/+/, "");
    if (leaf === "") return parent;
    return parent === "/" ? "/" + leaf : parent + "/" + leaf;
  }

  function parentContentPath(value) {
    const path = normalizeContentPath(value);
    const index = path.lastIndexOf("/");
    return index <= 0 ? "/" : path.slice(0, index);
  }

  function baseName(value) {
    const path = normalizeContentPath(value);
    const index = path.lastIndexOf("/");
    return index < 0 ? path : path.slice(index + 1);
  }

  function initialFileBrowserPath() {
    const parts = ((root.location && root.location.pathname) || "").split("/").filter(Boolean);
    if (parts.length > 2 && parts[1] === "files") return "/" + parts.slice(2).join("/");
    return "/";
  }

  function formatBytes(size) {
    const value = Number(size);
    if (!Number.isFinite(value) || value < 0) return "";
    if (value < 1024) return `${value} B`;
    if (value < 1024 * 1024) return `${(value / 1024).toFixed(1)} KB`;
    return `${(value / (1024 * 1024)).toFixed(1)} MB`;
  }

  // appendHighlighted renders an untrusted search snippet as text, turning only
  // Clio's private sentinels into <mark> elements. Escaping happens through
  // textContent (or text nodes), so indexed content cannot inject markup.
  function appendHighlighted(doc, parent, snippet) {
    const parts = String(snippet == null ? "" : snippet).split(/(\u27e6|\u27e7)/);
    let marking = false;
    for (const part of parts) {
      if (part === "\u27e6") { marking = true; continue; }
      if (part === "\u27e7") { marking = false; continue; }
      if (part === "") continue;
      if (marking) {
        const mark = doc.createElement("mark");
        mark.textContent = part;
        parent.appendChild(mark);
      } else if (doc.createTextNode) {
        parent.appendChild(doc.createTextNode(part));
      } else {
        const span = doc.createElement("span");
        span.textContent = part;
        parent.appendChild(span);
      }
    }
  }

  // FileBrowser is a classic file explorer: a lazy-loading folder tree in the
  // left pane, a compact detailed list in the right pane and a staged upload
  // tray. Folder names and staged values are assigned as text (textContent), so
  // server data is never rendered as HTML.
  const FileBrowser = {
    mount(target, options) {
      options = options || {};
      const doc = root.document;
      if (!doc) throw new TypeError("Clio.FileBrowser.mount() requires a browser element");
      const host = typeof target === "string" ? doc.querySelector(target) : target;
      if (!host) throw new TypeError("Clio.FileBrowser.mount() requires a browser element");
      const project = fileBrowserProject(options);
      const client = options.client || new Clio(Object.assign({}, options, { project }));
      const uploadLimit = Number(options.uploadLimit) > 0 ? Number(options.uploadLimit) : 16 * 1024 * 1024;
      let currentPath = normalizeContentPath(options.path || initialFileBrowserPath());
      let currentChildren = [];
      let selectedEntry = "";
      let requestNumber = 0;
      let destroyed = false;

      const element = (tag, text, className) => {
        const value = doc.createElement(tag);
        if (text != null) value.textContent = String(text);
        if (className) value.className = className;
        return value;
      };
      const ask = (message, value) => (typeof root.prompt === "function" ? root.prompt(message, value) : null);
      const confirmDelete = (name) => (typeof root.confirm === "function" ? root.confirm(`Delete ${name}?`) : true);

      const toolbar = element("div", null, "fb-toolbar");
      const searchForm = element("form");
      const searchInput = element("input");
      searchInput.type = "search";
      searchInput.setAttribute("placeholder", "Search pages and files");
      searchInput.setAttribute("aria-label", "Search pages and files");
      const searchButton = element("button", "Search");
      searchButton.type = "submit";
      searchForm.append(searchInput, searchButton);
      const newFolderButton = element("button", "New folder");
      newFolderButton.type = "button";
      const uploadButton = element("button", "Upload…", "fb-upload-button");
      uploadButton.type = "button";
      const uploadInput = element("input");
      uploadInput.type = "file";
      uploadInput.multiple = true;
      uploadInput.hidden = true;
      uploadInput.setAttribute("aria-label", "Choose files to stage");
      const projectSwitcher = appendProjectNav(doc, toolbar, project, (next) => {
        if (!next || next === project) return;
        if (!confirmDiscardStaged()) return;
        const sub = currentPath === "/" ? "/files" : "/files" + encodeURI(currentPath);
        switchProject(next, sub);
      });
      toolbar.append(searchForm, newFolderButton, uploadButton, uploadInput);

      const breadcrumbs = element("nav", null, "fb-breadcrumbs");
      breadcrumbs.setAttribute("aria-label", "Breadcrumb");
      const status = element("p", "Loading…", "fb-status");
      status.setAttribute("role", "status");
      const staging = element("section", null, "fb-staging");
      staging.setAttribute("aria-label", "Staged uploads");
      staging.hidden = true;
      const layout = element("div", null, "fb-layout");
      const treePane = element("aside", null, "fb-tree-pane");
      treePane.appendChild(element("h2", "Folders", "fb-tree-heading"));
      const treeContainer = element("div", null, "fb-tree");
      treeContainer.setAttribute("role", "tree");
      treeContainer.setAttribute("aria-label", "Folder tree");
      treePane.appendChild(treeContainer);
      const contentArea = element("section", null, "fb-content");
      contentArea.setAttribute("aria-label", "Folder contents");
      const list = element("div", null, "fb-list");
      contentArea.appendChild(list);
      layout.append(treePane, contentArea);
      host.replaceChildren(toolbar, breadcrumbs, status, staging, layout);

      // --- lazy folder tree -------------------------------------------------
      const treeNodes = new Map();
      let focusIndex = 0;
      let lastFocused = null;
      const treeStorageKey = `clio-filebrowser-tree:${project}`;
      // The project root is always expanded so the tree is usable immediately.
      ensureTreeNode("/").expanded = true;

      function treeName(path) {
        const value = normalizeContentPath(path);
        if (value === "/") return project;
        const parts = value.split("/").filter(Boolean);
        return parts.length ? parts[parts.length - 1] : project;
      }

      function ensureTreeNode(path) {
        const value = normalizeContentPath(path);
        let node = treeNodes.get(value);
        if (!node) {
          node = { path: value, name: treeName(value), loaded: false, loading: false, expanded: false, children: [], rawCount: 0, total: 0, error: "" };
          treeNodes.set(value, node);
        }
        return node;
      }

      function readExpandedPaths() {
        try {
          const raw = root.sessionStorage ? root.sessionStorage.getItem(treeStorageKey) : null;
          if (!raw) return [];
          const parsed = JSON.parse(raw);
          return Array.isArray(parsed) ? parsed.filter((item) => typeof item === "string") : [];
        } catch (_) {
          return [];
        }
      }

      function saveExpandedPaths() {
        try {
          if (!root.sessionStorage) return;
          const paths = [];
          for (const node of treeNodes.values()) if (node.expanded) paths.push(node.path);
          root.sessionStorage.setItem(treeStorageKey, JSON.stringify(paths));
        } catch (_) {}
      }

      function directoryChildren(listing, base) {
        const result = [];
        const children = listing && Array.isArray(listing.children) ? listing.children : [];
        for (const child of children) {
          if (!child || child.kind !== "directory") continue;
          const childPath = String(child.path || joinContentPath(base, child.name));
          const node = ensureTreeNode(childPath);
          if (child.name != null) node.name = String(child.name);
          result.push(childPath);
        }
        return result;
      }

      function visibleTreeItems() {
        const items = [];
        const walk = (path, level) => {
          const node = ensureTreeNode(path);
          items.push({ path, node, level, more: false });
          if (!node.expanded) return;
          for (const childPath of node.children) walk(childPath, level + 1);
          if (node.loaded && node.rawCount < node.total) items.push({ path, node, level: level + 1, more: true });
        };
        walk("/", 1);
        return items;
      }

      function treeIndent(level) {
        const indent = element("span", null, "fb-tree-indent");
        for (let i = 1; i < level; i++) indent.appendChild(element("span", null, "fb-tree-guide"));
        return indent;
      }

      function renderTree() {
        treeContainer.replaceChildren();
        const items = visibleTreeItems();
        if (focusIndex > items.length - 1) focusIndex = Math.max(0, items.length - 1);
        lastFocused = null;
        items.forEach((item, index) => {
          const node = item.node;
          const isCurrent = !item.more && item.path === currentPath;
          const row = element("div");
          row.className = "fb-tree-item" + (isCurrent ? " fb-tree-current" : "") + (item.more ? " fb-tree-more" : "");
          row.setAttribute("role", "treeitem");
          row.setAttribute("aria-level", String(item.level));
          row.setAttribute("aria-selected", isCurrent ? "true" : "false");
          row.tabIndex = index === focusIndex ? 0 : -1;
          row.setAttribute("tabindex", String(index === focusIndex ? 0 : -1));
          if (index === focusIndex) lastFocused = row;
          row.appendChild(treeIndent(item.level));
          if (item.more) {
            const more = element("button", "Load more…", "fb-tree-more-button");
            more.type = "button";
            more.addEventListener("click", (event) => {
              if (event && event.preventDefault) event.preventDefault();
              loadMoreTreeNode(item.path, node);
            });
            row.appendChild(more);
          } else {
            row.setAttribute("data-path", item.path);
            row.setAttribute("aria-expanded", node.expanded ? "true" : "false");
            if (node.loaded && node.children.length === 0) {
              row.appendChild(element("span", null, "fb-tree-spacer"));
            } else {
              const chevron = element("button", node.loading ? "…" : (node.expanded ? "▼" : "▶"), "fb-tree-toggle" + (node.loading ? " fb-tree-loading" : ""));
              chevron.type = "button";
              chevron.setAttribute("aria-label", `${node.loading ? "Loading" : (node.expanded ? "Collapse" : "Expand")} ${node.name}`);
              chevron.addEventListener("click", (event) => {
                if (event && event.preventDefault) event.preventDefault();
                toggleTreeNode(item.path);
              });
              row.appendChild(chevron);
            }
            const label = element("button", node.name, "fb-tree-label");
            label.type = "button";
            label.setAttribute("aria-label", `Open ${node.name}`);
            label.addEventListener("click", (event) => {
              if (event && event.preventDefault) event.preventDefault();
              requestNavigate(item.path, false);
            });
            row.appendChild(label);
          }
          treeContainer.appendChild(row);
        });
      }

      async function loadTreeNode(path, offset) {
        const node = ensureTreeNode(path);
        if (node.loading) return;
        offset = offset || 0;
        node.loading = true;
        renderTree();
        try {
          const listing = await client.directory(path, { limit: 1000, offset });
          if (destroyed) return;
          const dirs = directoryChildren(listing, path);
          node.children = offset > 0 ? node.children.concat(dirs) : dirs;
          const children = listing && Array.isArray(listing.children) ? listing.children : [];
          const page = (listing && listing.page) || {};
          node.rawCount = offset + children.length;
          node.total = Number(page.total != null ? page.total : node.rawCount) || node.rawCount;
          node.loaded = true;
          node.error = "";
        } catch (error) {
          if (destroyed) return;
          node.loaded = true;
          node.error = error && error.message ? error.message : "Unable to load this folder.";
        } finally {
          node.loading = false;
        }
        if (!destroyed) renderTree();
      }

      async function toggleTreeNode(path) {
        const node = ensureTreeNode(path);
        if (node.expanded) {
          node.expanded = false;
          saveExpandedPaths();
          renderTree();
          return;
        }
        node.expanded = true;
        saveExpandedPaths();
        renderTree();
        if (!node.loaded) await loadTreeNode(path);
      }

      function loadMoreTreeNode(path, node) {
        if (node && node.loading) return;
        return loadTreeNode(path, node ? node.rawCount : 0);
      }

      async function ensureAncestors(path) {
        const value = normalizeContentPath(path);
        const parts = value.split("/").filter(Boolean);
        const ancestors = ["/"];
        let current = "/";
        for (let i = 0; i < parts.length - 1; i++) {
          current = current === "/" ? "/" + parts[i] : current + "/" + parts[i];
          ancestors.push(current);
        }
        for (const ancestor of ancestors) {
          const node = ensureTreeNode(ancestor);
          node.expanded = true;
          if (!node.loaded && !node.loading) await loadTreeNode(ancestor);
        }
      }

      function seedTreeNode(path, listing) {
        const node = ensureTreeNode(path);
        node.children = directoryChildren(listing, path);
        const children = listing && Array.isArray(listing.children) ? listing.children : [];
        const page = (listing && listing.page) || {};
        node.rawCount = children.length;
        node.total = Number(page.total != null ? page.total : children.length) || children.length;
        node.loaded = true;
        node.error = "";
      }

      function setFocus(index, items) {
        focusIndex = Math.max(0, Math.min(index, items.length - 1));
        renderTree();
        if (lastFocused && typeof lastFocused.focus === "function") lastFocused.focus();
      }

      function focusedItem(items) {
        if (!items.length) return null;
        return items[Math.max(0, Math.min(focusIndex, items.length - 1))];
      }

      function activateFocused(items) {
        const item = focusedItem(items);
        if (!item) return;
        if (item.more) { loadMoreTreeNode(item.path, item.node); return; }
        requestNavigate(item.path, false);
      }

      function expandFocused(items) {
        const item = focusedItem(items);
        if (!item || item.more) return;
        if (!item.node.expanded) { toggleTreeNode(item.path); return; }
        if (item.node.children.length) setFocus(focusIndex + 1, items);
      }

      function collapseFocused(items) {
        const item = focusedItem(items);
        if (!item || item.more) return;
        if (item.node.expanded) { toggleTreeNode(item.path); return; }
        const parent = parentContentPath(item.path);
        const index = items.findIndex((candidate) => !candidate.more && candidate.path === parent);
        if (index >= 0) setFocus(index, items);
      }

      treeContainer.addEventListener("keydown", (event) => {
        const key = event && event.key;
        if (!key) return;
        const items = visibleTreeItems();
        if (!items.length) return;
        if (key === "ArrowDown" || key === "ArrowUp") {
          if (event.preventDefault) event.preventDefault();
          setFocus(focusIndex + (key === "ArrowDown" ? 1 : -1), items);
        } else if (key === "ArrowRight") {
          if (event.preventDefault) event.preventDefault();
          expandFocused(items);
        } else if (key === "ArrowLeft") {
          if (event.preventDefault) event.preventDefault();
          collapseFocused(items);
        } else if (key === "Enter") {
          if (event.preventDefault) event.preventDefault();
          activateFocused(items);
        }
      });

      // --- staged upload tray -----------------------------------------------
      let tray = [];
      let stagingSeq = 0;
      let uploading = false;

      function sanitizeUploadName(raw) {
        let name = String(raw == null ? "" : raw).replace(/\\/g, "/");
        const parts = name.split("/");
        name = parts[parts.length - 1];
        name = name.replace(/[\u0000-\u001f\u007f]/g, "").trim();
        if (name === "" || name === "." || name === "..") return "";
        return name;
      }

      function existingNames() {
        const names = new Set();
        for (const child of currentChildren) {
          const name = child && child.name != null ? String(child.name) : treeName(String((child && child.path) || ""));
          if (name) names.add(name);
        }
        return names;
      }

      function revalidateStaging() {
        const names = existingNames();
        for (const item of tray) {
          if (item.status === "done") continue;
          item.replaces = names.has(item.name);
        }
        renderStaging();
      }

      function stageFiles(fileList) {
        const files = fileList ? Array.prototype.slice.call(fileList) : [];
        let rejected = 0;
        let staged = 0;
        for (const file of files) {
          if (!file) continue;
          const size = Number(file.size);
          if (Number.isFinite(size) && size > uploadLimit) { rejected++; continue; }
          const name = sanitizeUploadName(file.name != null ? file.name : "upload");
          if (!name) { rejected++; continue; }
          tray.push({
            key: ++stagingSeq,
            file,
            name,
            size: Number.isFinite(size) ? size : 0,
            type: String(file.type || ""),
            target: joinContentPath(currentPath, name),
            status: "queued",
            error: "",
            replaces: false
          });
          staged++;
        }
        revalidateStaging();
        if (rejected > 0) {
          status.className = "fb-status fb-error";
          status.textContent = `${rejected} file(s) were not staged: files must be no larger than 16 MiB and have a usable name.`;
        } else if (staged > 0) {
          status.className = "fb-status";
          status.textContent = `${staged} file(s) staged. Choose Upload to send them.`;
        }
        return staged;
      }

      function clearStaging() {
        tray = [];
        renderStaging();
      }

      function confirmDiscardStaged() {
        if (!tray.length) return true;
        const message = `Discard ${tray.length} staged upload${tray.length === 1 ? "" : "s"}?`;
        const confirmed = typeof root.confirm === "function" ? root.confirm(message) : true;
        if (!confirmed) return false;
        clearStaging();
        return true;
      }

      function renderStaging() {
        staging.replaceChildren();
        if (!tray.length) {
          staging.hidden = true;
          return;
        }
        staging.hidden = false;
        staging.appendChild(element("p", "Staged uploads", "fb-staging-heading"));
        const rows = element("div", null, "fb-staging-rows");
        for (const item of tray) {
          const row = element("div", null, "fb-staging-row");
          row.setAttribute("data-status", item.status);
          row.setAttribute("data-replaces", item.replaces ? "true" : "false");
          row.appendChild(element("span", item.name, "fb-staging-name"));
          row.appendChild(element("span", formatBytes(item.size), "fb-staging-size"));
          row.appendChild(element("span", item.type || "application/octet-stream", "fb-staging-type"));
          row.appendChild(element("span", item.target, "fb-staging-target"));
          let label = item.status;
          if (item.status === "error") label = `error: ${item.error || "upload failed"}`;
          else if (item.replaces) label = `${item.status} · will replace`;
          row.appendChild(element("span", label, "fb-staging-status"));
          const remove = element("button", "×", "fb-staging-remove");
          remove.type = "button";
          remove.disabled = uploading;
          remove.setAttribute("aria-label", `Remove ${item.name} from the staged uploads`);
          remove.addEventListener("click", (event) => {
            if (event && event.preventDefault) event.preventDefault();
            tray = tray.filter((entry) => entry !== item);
            renderStaging();
          });
          row.appendChild(remove);
          rows.appendChild(row);
        }
        const actions = element("div", null, "fb-staging-actions");
        const upload = element("button", `Upload ${tray.length} file${tray.length === 1 ? "" : "s"}`, "fb-staging-upload");
        upload.type = "button";
        upload.disabled = uploading || tray.length === 0;
        upload.addEventListener("click", (event) => {
          if (event && event.preventDefault) event.preventDefault();
          uploadStaged();
        });
        const clear = element("button", "Clear", "fb-staging-clear");
        clear.type = "button";
        clear.disabled = uploading;
        clear.addEventListener("click", (event) => {
          if (event && event.preventDefault) event.preventDefault();
          clearStaging();
        });
        actions.append(upload, clear);
        staging.append(rows, actions);
      }

      async function uploadStaged() {
        if (uploading || !tray.length) return;
        const pending = tray.filter((item) => item.status !== "done");
        if (!pending.length) return;
        if (pending.some((item) => item.replaces)) {
          const message = "Some staged files already exist and will be replaced. Continue?";
          const confirmed = typeof root.confirm === "function" ? root.confirm(message) : true;
          if (!confirmed) return;
        }
        uploading = true;
        renderStaging();
        let index = 0;
        let changed = false;
        for (const item of pending) {
          if (destroyed) break;
          index++;
          item.status = "uploading";
          item.error = "";
          renderStaging();
          status.className = "fb-status";
          status.textContent = `Uploading ${index} of ${pending.length}…`;
          try {
            await client.putFile(item.target, item.file, { contentType: item.type });
            changed = true;
            tray = tray.filter((entry) => entry !== item);
          } catch (error) {
            item.status = "error";
            item.error = error && error.message ? error.message : "Upload failed.";
          }
          if (!destroyed) renderStaging();
        }
        uploading = false;
        if (destroyed) return;
        renderStaging();
        if (changed) await loadDirectory(currentPath, true);
        if (destroyed) return;
        const failed = tray.length;
        if (failed) {
          status.className = "fb-status fb-error";
          status.textContent = `${failed} upload(s) failed. Fix or remove them and try again.`;
        }
      }

      // --- navigation and listing -------------------------------------------
      function setURL(path, replace) {
        const base = `/${encodeURIComponent(project)}/files`;
        const url = path === "/" ? base : base + encodeURI(path);
        const history = root.history;
        if (history && history[replace ? "replaceState" : "pushState"]) {
          history[replace ? "replaceState" : "pushState"]({}, "", url);
        }
      }

      function drawBreadcrumbs() {
        breadcrumbs.replaceChildren();
        const rootLink = element("a", project);
        rootLink.href = `/${encodeURIComponent(project)}/files`;
        rootLink.addEventListener("click", (event) => {
          if (event && event.preventDefault) event.preventDefault();
          requestNavigate("/", false);
        });
        breadcrumbs.appendChild(rootLink);
        let accumulated = "";
        for (const part of currentPath.split("/").filter(Boolean)) {
          accumulated += "/" + part;
          const target = accumulated;
          breadcrumbs.appendChild(element("span", "/"));
          const link = element("a", part);
          link.href = `/${encodeURIComponent(project)}/files${encodeURI(target)}`;
          link.addEventListener("click", (event) => {
            if (event && event.preventDefault) event.preventDefault();
            requestNavigate(target, false);
          });
          breadcrumbs.appendChild(link);
        }
      }

      function openEntry(child, childPath) {
        if (child && child.kind === "directory") {
          requestNavigate(childPath, false);
          return;
        }
        const url = child && child.url ? String(child.url) : `/${encodeURIComponent(project)}/files${encodeURI(childPath)}`;
        if (typeof root.open === "function") root.open(url, "_blank");
        else if (root.location && "href" in root.location) root.location.href = url;
      }

      function entryActions(name, childPath) {
        const actions = element("div", null, "fb-actions");
        const renameButton = element("button", "Rename");
        renameButton.type = "button";
        renameButton.addEventListener("click", (event) => {
          if (event && event.preventDefault) event.preventDefault();
          const next = ask("New name", name);
          if (next) renameEntry(childPath, joinContentPath(parentContentPath(childPath), next));
        });
        const deleteButton = element("button", "Delete", "fb-danger");
        deleteButton.type = "button";
        deleteButton.addEventListener("click", (event) => {
          if (event && event.preventDefault) event.preventDefault();
          if (confirmDelete(name)) removeEntry(childPath);
        });
        actions.append(renameButton, deleteButton);
        return actions;
      }

      function drawEntries(children) {
        list.replaceChildren();
        if (!children.length) {
          list.appendChild(element("p", "This folder is empty.", "fb-empty"));
          return;
        }
        const directories = [];
        const files = [];
        for (const child of children) {
          const childPath = String(child.path || joinContentPath(currentPath, child.name || ""));
          const item = { child, childPath, name: baseName(childPath) };
          if (child && child.kind === "directory") directories.push(item);
          else files.push(item);
        }
        // Subfolders first, as a compact list with a folder icon.
        if (directories.length) {
          const section = element("section", null, "fb-dirs");
          section.setAttribute("aria-label", "Subfolders");
          for (const item of directories) {
            const row = element("div", null, "fb-dir-row");
            const open = element("button", null, "fb-dir");
            open.type = "button";
            open.setAttribute("aria-label", `Open folder ${item.name}`);
            open.append(element("span", "📁", "fb-dir-icon"), element("span", item.name, "fb-dir-name"));
            open.addEventListener("click", (event) => {
              if (event && event.preventDefault) event.preventDefault();
              requestNavigate(item.childPath, false);
            });
            row.append(open, entryActions(item.name, item.childPath));
            section.appendChild(row);
          }
          list.appendChild(section);
        }
        // Files below: Name (basename), Size, Actions. No kind column and no
        // explicit download link (double-click opens/serves the entry).
        if (files.length) {
          const table = element("table", null, "fb-table");
          const head = element("thead");
          const headingRow = element("tr");
          for (const label of ["Name", "Size", ""]) headingRow.appendChild(element("th", label));
          head.appendChild(headingRow);
          table.appendChild(head);
          const body = element("tbody");
          for (const item of files) {
            const { child, childPath, name } = item;
            const row = element("tr", null, childPath === selectedEntry ? "fb-row-selected" : "");
            row.setAttribute("data-path", childPath);
            const nameCell = element("td", null, "fb-name-cell");
            const link = element("a", name);
            link.href = child.url ? String(child.url) : `/${encodeURIComponent(project)}/files${encodeURI(childPath)}`;
            link.addEventListener("click", (event) => {
              if (event && event.preventDefault) event.preventDefault();
              selectedEntry = childPath;
              drawEntries(currentChildren);
            });
            link.addEventListener("dblclick", (event) => {
              if (event && event.preventDefault) event.preventDefault();
              openEntry(child, childPath);
            });
            nameCell.appendChild(link);
            row.appendChild(nameCell);
            row.appendChild(element("td", formatBytes(child.size), "fb-size-cell"));
            const actionsCell = element("td", null, "fb-actions-cell");
            actionsCell.appendChild(entryActions(name, childPath));
            row.appendChild(actionsCell);
            row.addEventListener("click", () => {
              selectedEntry = childPath;
              drawEntries(currentChildren);
            });
            row.addEventListener("dblclick", (event) => {
              if (event && event.preventDefault) event.preventDefault();
              openEntry(child, childPath);
            });
            body.appendChild(row);
          }
          table.appendChild(body);
          list.appendChild(table);
        }
      }

      function action(promise, failure) {
        status.className = "fb-status";
        status.textContent = "Working…";
        return Promise.resolve(promise).then(
          () => {
            if (!destroyed) return loadDirectory(currentPath, true);
          },
          (error) => {
            if (destroyed) return;
            status.className = "fb-status fb-error";
            status.textContent = error && error.message ? error.message : (failure || "Request failed.");
          }
        );
      }

      function createFolder(name, parent) {
        const value = String(name == null ? "" : name).trim();
        if (!value) return Promise.resolve();
        return action(client.createDirectory(joinContentPath(parent == null ? currentPath : parent, value)), "Could not create the folder.");
      }

      function upload(file, name, parent) {
        if (!file) return Promise.resolve();
        const fileName = sanitizeUploadName(name || file.name || "upload") || "upload";
        const target = joinContentPath(parent == null ? currentPath : parent, fileName);
        return action(client.putFile(target, file, { contentType: file.type || "" }), "Could not upload the file.");
      }

      function renameEntry(from, to) {
        if (!from || !to || from === to) return Promise.resolve();
        return action(client.moveFile(from, to), "Could not rename the entry.");
      }

      function removeEntry(path) {
        if (!path) return Promise.resolve();
        return action(client.deleteFile(path), "Could not delete the entry.");
      }

      async function runSearch(query) {
        const value = String(query == null ? searchInput.value : query).trim();
        const request = ++requestNumber;
        if (!value) return loadDirectory(currentPath, true);
        status.className = "fb-status";
        status.textContent = "Searching…";
        list.replaceChildren();
        try {
          const result = await client.search(value);
          if (destroyed || request !== requestNumber) return;
          const items = result && Array.isArray(result.data) ? result.data : [];
          status.textContent = `${items.length} result(s) for “${value}”`;
          const container = element("div", null, "fb-search-results");
          for (const item of items) {
            const article = element("article", null, "fb-result");
            const link = element("a", item.path);
            link.href = `/${encodeURIComponent(project)}/files${encodeURI(String(item.path))}`;
            article.appendChild(link);
            article.appendChild(element("p", `${item.kind} · ${item.content_type} · ${item.source}`, "fb-result-meta"));
            const snippet = element("p");
            appendHighlighted(doc, snippet, item.snippet);
            article.appendChild(snippet);
            container.appendChild(article);
          }
          if (!items.length) container.appendChild(element("p", "No pages or files matched.", "fb-empty"));
          list.appendChild(container);
        } catch (error) {
          if (destroyed || request !== requestNumber) return;
          status.className = "fb-status fb-error";
          status.textContent = error && error.message ? error.message : "Search failed.";
        }
      }

      async function loadDirectory(path, replaceURL) {
        const target = normalizeContentPath(path == null ? "/" : path);
        const request = ++requestNumber;
        status.className = "fb-status";
        status.textContent = "Loading…";
        try {
          const listing = await client.directory(target);
          if (destroyed || request !== requestNumber) return;
          if (!listing || listing.kind !== "directory") {
            const parent = parentContentPath(target);
            if (parent !== target) return loadDirectory(parent, replaceURL);
            throw new TypeError("Path is not a directory");
          }
          currentPath = normalizeContentPath(listing.path || target);
          currentChildren = Array.isArray(listing.children) ? listing.children : [];
          const node = ensureTreeNode(currentPath);
          if (node.expanded) seedTreeNode(currentPath, listing);
          await ensureAncestors(currentPath);
          if (destroyed || request !== requestNumber) return;
          const items = visibleTreeItems();
          const index = items.findIndex((candidate) => !candidate.more && candidate.path === currentPath);
          if (index >= 0) focusIndex = index;
          drawBreadcrumbs();
          renderTree();
          drawEntries(currentChildren);
          revalidateStaging();
          status.textContent = `${currentChildren.length} item(s)`;
          searchInput.value = "";
          setURL(currentPath, replaceURL === true);
        } catch (error) {
          if (destroyed || request !== requestNumber) return;
          status.className = "fb-status fb-error";
          status.textContent = error && error.message ? error.message : "Unable to load this folder.";
        }
      }

      function requestNavigate(path, replaceURL) {
        if (destroyed) return Promise.resolve();
        if (!confirmDiscardStaged()) return Promise.resolve();
        return loadDirectory(path, replaceURL);
      }

      // --- wiring -----------------------------------------------------------
      searchForm.addEventListener("submit", (event) => {
        if (event && event.preventDefault) event.preventDefault();
        runSearch(searchInput.value);
      });
      newFolderButton.addEventListener("click", (event) => {
        if (event && event.preventDefault) event.preventDefault();
        const name = ask("Folder name", "");
        if (name) createFolder(name, currentPath);
      });
      uploadButton.addEventListener("click", () => {
        if (typeof uploadInput.click === "function") uploadInput.click();
      });
      uploadInput.addEventListener("change", () => {
        stageFiles(uploadInput.files);
        try { uploadInput.value = ""; } catch (_) {}
      });
      list.addEventListener("dragover", (event) => {
        if (event && event.preventDefault) event.preventDefault();
        list.className = "fb-list fb-drop";
      });
      list.addEventListener("dragleave", () => { list.className = "fb-list"; });
      list.addEventListener("drop", (event) => {
        if (event && event.preventDefault) event.preventDefault();
        list.className = "fb-list";
        const files = event && event.dataTransfer ? event.dataTransfer.files : null;
        if (files && files.length) stageFiles(files);
      });
      const popstate = () => requestNavigate(initialFileBrowserPath(), true);
      if (root.addEventListener) root.addEventListener("popstate", popstate);

      // Restore the persisted expansion state, then load the tree.
      const persisted = readExpandedPaths();
      for (const path of persisted) ensureTreeNode(path).expanded = true;
      const ready = loadDirectory(currentPath, true).then(async () => {
        for (const path of persisted) {
          const node = ensureTreeNode(path);
          if (node.expanded && !node.loaded && !node.loading) await loadTreeNode(path);
        }
      });
      const projectsReady = projectSwitcher
        ? fillProjectSwitcher(doc, projectSwitcher.select, client, project)
        : Promise.resolve();
      return {
        ready: Promise.all([ready, projectsReady]).then(() => undefined),
        get path() { return currentPath; },
        refresh() { return loadDirectory(currentPath, true); },
        navigate(path) { return requestNavigate(path, false); },
        search(query) { return runSearch(query); },
        createFolder(name, parent) { return createFolder(name, parent); },
        upload(file, name, parent) { return upload(file, name, parent); },
        rename(from, to) { return renameEntry(from, to); },
        remove(path) { return removeEntry(path); },
        stage(files) { return stageFiles(files); },
        uploadStaged() { return uploadStaged(); },
        clearStaging() { return clearStaging(); },
        get staged() { return tray.map((item) => ({ name: item.name, size: item.size, type: item.type, target: item.target, status: item.status, replaces: item.replaces })); },
        destroy() {
          destroyed = true;
          if (root.removeEventListener) root.removeEventListener("popstate", popstate);
          host.replaceChildren();
        }
      };
    }
  };

  // Projects is an interactive manager over the instance-level projects API
  // (section 65.5). It lists, creates and deletes projects without adding a
  // human route: every link it renders is project-scoped (`/{name}/`). Names,
  // labels and descriptions are assigned as text, so server data is never
  // rendered as HTML.
  const Projects = {
    mount(target, options) {
      options = options || {};
      const doc = root.document;
      if (!doc) throw new TypeError("Clio.Projects.mount() requires a browser element");
      const host = typeof target === "string" ? doc.querySelector(target) : target;
      if (!host) throw new TypeError("Clio.Projects.mount() requires a browser element");
      const currentProject = options.project ? String(options.project) : currentProjectFromLocation();
      const client = options.client || new Clio(Object.assign({}, options, { project: currentProject }));
      let destroyed = false;

      const element = (tag, text, className) => {
        const value = doc.createElement(tag);
        if (text != null) value.textContent = String(text);
        if (className) value.className = className;
        return value;
      };
      const text = (value) => (doc.createTextNode ? doc.createTextNode(String(value)) : element("span", value));

      // Only the default project exposes the projects manager. Mounted on any
      // other project, it offers a single Home link to bare "/" and no list,
      // switcher or create/delete controls, so a non-default project never
      // links to a sibling project (section 66.1).
      if (currentProject !== "default") {
        const home = element("a", "Home", "project-home");
        home.href = "/";
        host.replaceChildren(home);
        return {
          ready: Promise.resolve(),
          refresh() { return Promise.resolve(); },
          create() { return Promise.resolve(); },
          remove() { return Promise.resolve(); },
          destroy() {
            destroyed = true;
            host.replaceChildren();
          }
        };
      }

      const status = element("p", "", "projects-status");
      status.setAttribute("role", "status");
      const list = element("ul", null, "projects-list");
      const form = element("form", null, "projects-create");

      const nameLabel = element("label", "Name");
      const nameInput = element("input");
      nameInput.type = "text";
      nameInput.name = "name";
      nameInput.required = true;
      nameInput.setAttribute("placeholder", "project-name");
      nameInput.setAttribute("pattern", "[a-z0-9][a-z0-9_-]*");
      nameLabel.appendChild(nameInput);
      const labelLabel = element("label", "Label (optional)");
      const labelInput = element("input");
      labelInput.type = "text";
      labelInput.name = "label";
      labelLabel.appendChild(labelInput);
      const descriptionLabel = element("label", "Description (optional)");
      const descriptionInput = element("input");
      descriptionInput.type = "text";
      descriptionInput.name = "description";
      descriptionLabel.appendChild(descriptionInput);
      const orderLabel = element("label", "Order (optional)");
      const orderInput = element("input");
      orderInput.type = "number";
      orderInput.name = "order";
      orderLabel.appendChild(orderInput);
      const submit = element("button", "Create project");
      submit.type = "submit";
      form.append(nameLabel, labelLabel, descriptionLabel, orderLabel, submit);

      host.replaceChildren(element("h2", "Projects"), status, list, form);

      function renderList(projects) {
        list.replaceChildren();
        for (const item of projects) {
          const name = String(item.name);
          const row = element("li", null, "projects-item");
          row.setAttribute("data-project", name);
          const link = element("a", item.label || name);
          link.href = `/${encodeURIComponent(name)}/`;
          row.appendChild(link);
          if (name !== "default") {
            row.appendChild(text(" "));
            row.appendChild(element("span", name, "projects-slug"));
          }
          if (name === currentProject) {
            row.appendChild(text(" "));
            row.appendChild(element("span", "(current)", "projects-current"));
          }
          if (name !== "default") {
            const remove = element("button", "Delete", "projects-danger");
            remove.type = "button";
            remove.setAttribute("aria-label", `Delete project ${name}`);
            remove.addEventListener("click", () => {
              if (typeof root.confirm === "function" && !root.confirm(`Delete project ${name}?`)) return;
              removeProject(name);
            });
            row.appendChild(remove);
          }
          list.appendChild(row);
        }
      }

      function showError(error, fallback) {
        status.className = "projects-status projects-error";
        status.textContent = error && error.message ? error.message : fallback;
      }

      async function refresh() {
        try {
          const projects = await client.projects();
          if (destroyed) return;
          renderList(Array.isArray(projects) ? projects : []);
          status.className = "projects-status";
          status.textContent = "";
        } catch (error) {
          if (destroyed) return;
          showError(error, "Unable to load projects.");
        }
      }

      async function createProject(event) {
        if (event && event.preventDefault) event.preventDefault();
        const payload = { name: String(nameInput.value || "").trim() };
        const labelValue = String(labelInput.value || "").trim();
        const description = String(descriptionInput.value || "").trim();
        const order = String(orderInput.value || "").trim();
        if (labelValue) payload.label = labelValue;
        if (description) payload.description = description;
        if (order !== "") payload.order = Number(order);
        status.className = "projects-status";
        status.textContent = "Creating…";
        try {
          const created = await client.createProject(payload);
          if (destroyed) return;
          if (typeof form.reset === "function") form.reset();
          nameInput.value = "";
          labelInput.value = "";
          descriptionInput.value = "";
          orderInput.value = "";
          await refresh();
          status.className = "projects-status";
          status.textContent = `Created ${created && created.name ? created.name : payload.name}.`;
          if (root.location && typeof root.location.reload === "function") root.location.reload();
        } catch (error) {
          if (destroyed) return;
          showError(error, "Could not create the project.");
        }
      }

      async function removeProject(name) {
        if (name === "default") return;
        status.className = "projects-status";
        status.textContent = "Deleting…";
        try {
          await client.deleteProject(name);
          if (destroyed) return;
          await refresh();
          status.className = "projects-status";
          status.textContent = `Deleted ${name}.`;
          if (root.location && typeof root.location.reload === "function") root.location.reload();
        } catch (error) {
          if (destroyed) return;
          showError(error, `Could not delete ${name}.`);
        }
      }

      form.addEventListener("submit", createProject);
      return {
        ready: refresh(),
        refresh,
        create(value) {
          value = value || {};
          nameInput.value = value.name != null ? String(value.name) : "";
          labelInput.value = value.label != null ? String(value.label) : "";
          descriptionInput.value = value.description != null ? String(value.description) : "";
          orderInput.value = value.order != null ? String(value.order) : "";
          return createProject();
        },
        remove(name) { return removeProject(name); },
        destroy() {
          destroyed = true;
          host.replaceChildren();
        }
      };
    }
  };

  Object.defineProperty(Clio, "DataBrowser", { value: DataBrowser, enumerable: true });
  Object.defineProperty(Clio, "FileBrowser", { value: FileBrowser, enumerable: true });
  Object.defineProperty(Clio, "Projects", { value: Projects, enumerable: true });

  root.Clio = Clio;
})(typeof window !== "undefined" ? window : globalThis);

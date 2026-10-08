(function (root) {
  "use strict";

  const apiVersion = "v1";
  const libraryVersion = "1.0.0";

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

    directory(path, options) {
      options = Object.assign({}, options, { path });
      return this._get("/files/directories", options);
    }

    page(path, options) {
      options = Object.assign({}, options, { path });
      return this._get("/files/pages", options);
    }

    createDirectory(path, options) {
      return this._mutate("POST", "/files/directories", { path }, options);
    }

    deleteDirectory(path, options) {
      const query = queryString({ path });
      return this._request(`${this.apiUrl}/files/directories?${query}`, { method: "DELETE", signal: options && options.signal });
    }

    publishPage(value, options) {
      return this._mutate("POST", "/files/pages", value, options);
    }

    deletePage(path, options) {
      const query = queryString({ path });
      return this._request(`${this.apiUrl}/files/pages?${query}`, { method: "DELETE", signal: options && options.signal });
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
  // collection browser lives at /{project}/collections/..., so the project is
  // the first path segment.
  function browserProject(options) {
    if (options && options.project) return String(options.project);
    const parts = ((root.location && root.location.pathname) || "").split("/").filter(Boolean);
    if (parts.length > 1 && parts[1] === "collections") return parts[0];
    return "default";
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

      function setURL(group, table, page, replace) {
        const path = group ? `/${encodeURIComponent(project)}/collections/${encodeURIComponent(group)}${table ? `/${encodeURIComponent(table)}` : ""}` : `/${encodeURIComponent(project)}/collections`;
        const query = page > 1 ? `?page=${page}` : "";
        if (root.history && root.history[replace ? "replaceState" : "pushState"]) {
          root.history[replace ? "replaceState" : "pushState"]({}, "", `${path}${query}`);
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
          link.href = `/${encodeURIComponent(project)}/collections/${encodeURIComponent(currentGroup)}/${encodeURIComponent(table.name)}`;
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
          link.href = number > 1 ? `?page=${number}` : root.location.pathname;
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
      const initialParts = ((root.location && root.location.pathname) || "").split("/").filter(Boolean);
      const initialCollections = initialParts[1] === "collections";
      const initialGroup = initialCollections ? initialParts[2] || "" : "";
      const initialTable = initialCollections ? initialParts[3] || "" : "";
      const popstate = () => {
        const parts = (root.location.pathname || "").split("/").filter(Boolean);
        const onCollections = parts[1] === "collections";
        navigate(onCollections ? parts[2] || "" : "", onCollections ? parts[3] || "" : "", selectedPage(), true);
      };
      if (root.addEventListener) root.addEventListener("popstate", popstate);
      const ready = navigate(initialGroup, initialTable, selectedPage(), true);
      return {
        ready,
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

  Object.defineProperty(Clio, "DataBrowser", { value: DataBrowser, enumerable: true });

  root.Clio = Clio;
})(typeof window !== "undefined" ? window : globalThis);

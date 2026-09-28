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
      this.apiUrl = `${this.baseUrl}/api/${apiVersion}`;
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
      const response = await this._fetch(url, init);
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

    metadata(options) { return this._get("/metadata", options); }
    groups(options) { return this._get("/groups", options); }
    createGroup(value, options) { return this._mutate("POST", "/groups", value, options); }

    group(name) {
      const client = this;
      const groupName = name;
      const base = `/groups/${encodeURIComponent(name)}`;
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
      const base = `/groups/${encodeURIComponent(group)}/tables/${encodeURIComponent(name)}`;
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
      return this._get("/directories", options);
    }

    page(path, options) {
      options = Object.assign({}, options, { path });
      return this._get("/pages", options);
    }

    createDirectory(path, options) {
      return this._mutate("POST", "/directories", { path }, options);
    }

    deleteDirectory(path, options) {
      const query = queryString({ path });
      return this._request(`${this.apiUrl}/directories?${query}`, { method: "DELETE", signal: options && options.signal });
    }

    publishPage(value, options) {
      return this._mutate("POST", "/pages", value, options);
    }

    deletePage(path, options) {
      const query = queryString({ path });
      return this._request(`${this.apiUrl}/pages?${query}`, { method: "DELETE", signal: options && options.signal });
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

  root.Clio = Clio;
})(typeof window !== "undefined" ? window : globalThis);

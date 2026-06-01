(function () {
  'use strict';

  var currentSection = 'dashboard';
  var pollInterval = null;
  var statusData = null;
  var modalMode = 'add';
  var modalOriginalUrl = '';
  var chatBusy = false;

  document.addEventListener('DOMContentLoaded', init);

  function init() {
    document.querySelectorAll('nav a').forEach(function (a) {
      a.addEventListener('click', function (e) {
        e.preventDefault();
        navigate(a.dataset.section);
      });
    });

    window.addEventListener('popstate', function () {
      navigate(getSectionFromPath(), false);
    });

    document.getElementById('modal-cancel').addEventListener('click', closeModal);
    document.getElementById('modal-overlay').addEventListener('click', function (e) {
      if (e.target === e.currentTarget) closeModal();
    });
    document.getElementById('modal-form').addEventListener('submit', handleModalSubmit);

    document.getElementById('btn-add-server').addEventListener('click', openAddModal);
    document.getElementById('btn-edit-config').addEventListener('click', openConfigEditor);
    document.getElementById('btn-save-config').addEventListener('click', saveConfig);
    document.getElementById('btn-cancel-config').addEventListener('click', closeConfigEditor);
    document.getElementById('btn-copy-opencode').addEventListener('click', copyOpenCodeConfig);

    document.getElementById('btn-chat-send').addEventListener('click', sendChatMessage);
    document.getElementById('chat-model-select').addEventListener('change', updateSendButton);
    document.getElementById('chat-message-input').addEventListener('keydown', function (e) {
      if (e.key === 'Enter' && !e.shiftKey) {
        e.preventDefault();
        sendChatMessage();
      }
    });

    document.addEventListener('keydown', function (e) {
      if (e.key === 'Escape') {
        if (!document.getElementById('modal-overlay').classList.contains('hidden')) {
          closeModal();
        }
        if (!document.getElementById('config-editor').classList.contains('hidden')) {
          closeConfigEditor();
        }
      }
    });

    navigate(getSectionFromPath());
  }

  function getSectionFromPath() {
    var path = window.location.pathname.replace(/\/+$/, '');
    if (path.endsWith('/servers')) return 'servers';
    if (path.endsWith('/config')) return 'config';
    if (path.endsWith('/opencode')) return 'opencode';
    if (path.endsWith('/status')) return 'status';
    return 'dashboard';
  }

  function navigate(section, pushState) {
    if (pushState === undefined) pushState = true;
    currentSection = section;

    document.querySelectorAll('.page').forEach(function (p) {
      p.classList.remove('active');
    });
    document.querySelectorAll('nav a').forEach(function (a) {
      a.classList.remove('active');
    });

    document.getElementById('page-' + section).classList.add('active');
    document.querySelector('nav a[data-section="' + section + '"]').classList.add('active');

    if (pushState) {
      var path = section === 'dashboard' ? '/admin/' : '/admin/' + section;
      history.pushState(null, '', path);
    }

    if (section === 'dashboard') {
      loadDashboard();
      startPolling();
    } else {
      stopPolling();
      if (section === 'servers') loadServers();
      if (section === 'config') loadConfig();
      if (section === 'opencode') loadOpenCodeConfig();
      if (section === 'status') loadStatus();
      if (section === 'chat') loadModels();
    }
  }

  function showToast(message, type) {
    var container = document.getElementById('toast-container');
    var toast = document.createElement('div');
    toast.className = 'toast ' + type;
    toast.textContent = message;
    container.appendChild(toast);
    setTimeout(function () {
      toast.classList.add('fade-out');
      setTimeout(function () { toast.remove(); }, 300);
    }, 3000);
  }

  function escHtml(str) {
    if (str == null) return '';
    return String(str)
      .replace(/&/g, '&amp;')
      .replace(/</g, '&lt;')
      .replace(/>/g, '&gt;')
      .replace(/"/g, '&quot;')
      .replace(/'/g, '&#39;');
  }

  function fmtThroughput(tokensPerSec) {
    if (tokensPerSec == null || tokensPerSec <= 0) return '&mdash;';
    if (tokensPerSec < 1) return tokensPerSec.toFixed(2);
    if (tokensPerSec < 100) return tokensPerSec.toFixed(1);
    return Math.round(tokensPerSec).toString();
  }

  // ---------------------------------------------------------------------------
  // Polling
  // ---------------------------------------------------------------------------

  function startPolling() {
    if (pollInterval) return;
    pollInterval = setInterval(loadDashboard, 5000);
  }

  function stopPolling() {
    if (pollInterval) {
      clearInterval(pollInterval);
      pollInterval = null;
    }
  }

  // ---------------------------------------------------------------------------
  // Dashboard
  // ---------------------------------------------------------------------------

  function loadDashboard() {
    fetch('/admin/status')
      .then(function (res) {
        if (!res.ok) throw new Error('Status ' + res.status);
        return res.json();
      })
      .then(function (data) {
        statusData = data;
        renderDashboard(data);
      })
      .catch(function () {
        if (currentSection === 'dashboard') {
          showToast('Failed to load dashboard', 'error');
        }
      });
  }

  function renderDashboard(data) {
    var servers = data.servers || [];
    var modelsMap = data.models || {};

    var serverModels = {};
    for (var model in modelsMap) {
      if (modelsMap.hasOwnProperty(model)) {
        var urls = modelsMap[model];
        for (var i = 0; i < urls.length; i++) {
          if (!serverModels[urls[i]]) serverModels[urls[i]] = [];
          serverModels[urls[i]].push(model);
        }
      }
    }

    var total = servers.length;
    var healthyCount = 0;
    var unhealthyCount = 0;
    for (var j = 0; j < servers.length; j++) {
      if (servers[j].healthy) healthyCount++;
      else unhealthyCount++;
    }
    var totalModels = Object.keys(modelsMap).length;

    document.getElementById('dashboard-summary').innerHTML =
      '<div class="summary-card"><div class="label">Total Servers</div><div class="value">' +
      total + '</div></div>' +
      '<div class="summary-card"><div class="label">Healthy</div><div class="value" style="color:var(--green)">' +
      healthyCount + '</div></div>' +
      '<div class="summary-card"><div class="label">Unhealthy</div><div class="value" style="color:var(--red)">' +
      unhealthyCount + '</div></div>' +
      '<div class="summary-card"><div class="label">Total Models</div><div class="value">' +
      totalModels + '</div></div>';

    var cardsHtml = '';
    for (var k = 0; k < servers.length; k++) {
      var s = servers[k];
      var fillPct = s.max_concurrent_requests > 0
        ? Math.round((s.inflight / s.max_concurrent_requests) * 100)
        : 0;
      if (fillPct > 100) fillPct = 100;
      var statusClass = s.healthy ? 'healthy' : 'unhealthy';
      var models = serverModels[s.url] || [];
      var modelsHtml = '';
      if (models.length > 0) {
        modelsHtml = '<div class="card-row"><span class="card-row-label">Models:</span>' +
          '<div class="model-pills">';
        for (var m = 0; m < models.length; m++) {
          modelsHtml += '<span class="model-pill">' + escHtml(models[m]) + '</span>';
        }
        modelsHtml += '</div></div>';
      }

      // vLLM metrics if available
      var metricsHtml = '';
      if (s.metrics && s.metrics.updated_at) {
        var kvFill = Math.round(s.metrics.kv_cache_usage_perc);
        if (kvFill > 100) kvFill = 100;
        if (kvFill < 0) kvFill = 0;
        metricsHtml += '<div class="card-row">' +
          '<span class="card-row-label">KV Cache:</span>' +
          '<span>' + kvFill + '%</span>' +
          '<div class="progress-bar"><div class="progress-bar-fill progress-bar-fill-gpu" style="width:' + kvFill + '%"></div></div>' +
          '</div>';
        metricsHtml += '<div class="card-row">' +
          '<span class="card-row-label">Requests:</span>' +
          '<span>' + s.metrics.requests_running + ' run / ' + s.metrics.requests_waiting + ' wait</span>' +
          '</div>';
        metricsHtml += '<div class="card-row">' +
          '<span class="card-row-label">Tokens:</span>' +
          '<span>' + s.metrics.prompt_tokens_total + ' p / ' + s.metrics.gen_tokens_total + ' g</span>' +
          '</div>';
        metricsHtml += '<div class="card-row">' +
          '<span class="card-row-label">Speed:</span>' +
          '<span>' + fmtThroughput(s.metrics.prefill_throughput) + ' p/s &middot; ' +
          fmtThroughput(s.metrics.decode_throughput) + ' g/s</span>' +
          '</div>';
        metricsHtml = '<div class="card-metrics">' + metricsHtml + '</div>';
      }

      cardsHtml +=
        '<div class="server-card">' +
          '<div class="card-header">' +
            '<span class="status-dot ' + statusClass + '"></span>' +
            '<span class="card-name">' + escHtml(s.url) + '</span>' +
            '<span class="distance-badge">d=' + s.distance + '</span>' +
            '<button class="btn btn-danger btn-sm" onclick="window.__deleteServer(\'' +
              encodeURIComponent(s.url) + '\')">Delete</button>' +
          '</div>' +
          '<div class="card-body">' +
            '<div class="card-row">' +
              '<span class="card-row-label">Inflight:</span>' +
              '<span>' + s.inflight + ' / ' + s.max_concurrent_requests + '</span>' +
              '<div class="progress-bar"><div class="progress-bar-fill" style="width:' + fillPct + '%"></div></div>' +
            '</div>' +
            modelsHtml +
            metricsHtml +
          '</div>' +
        '</div>';
    }
    document.getElementById('dashboard-servers').innerHTML = cardsHtml;
  }

  // ---------------------------------------------------------------------------
  // Servers
  // ---------------------------------------------------------------------------

  function loadServers() {
    Promise.all([
      fetch('/admin/servers').then(function (r) { if (!r.ok) throw new Error(); return r.json(); }),
      fetch('/admin/status').then(function (r) { if (!r.ok) throw new Error(); return r.json(); })
    ])
      .then(function (results) {
        renderServersTable(results[0], results[1].models || {});
      })
      .catch(function () {
        showToast('Failed to load servers', 'error');
      });
  }

  function renderServersTable(servers, modelsMap) {
    var serverModels = {};
    for (var model in modelsMap) {
      if (modelsMap.hasOwnProperty(model)) {
        var urls = modelsMap[model];
        for (var i = 0; i < urls.length; i++) {
          if (!serverModels[urls[i]]) serverModels[urls[i]] = [];
          serverModels[urls[i]].push(model);
        }
      }
    }

    var tbody = document.getElementById('servers-tbody');
    var html = '';
    for (var j = 0; j < servers.length; j++) {
      var s = servers[j];
      var statusClass = s.healthy ? 'healthy' : 'unhealthy';
      var models = serverModels[s.url] || [];
      var modelsHtml = '';
      for (var m = 0; m < models.length; m++) {
        if (m > 0) modelsHtml += ', ';
        modelsHtml += escHtml(models[m]);
      }

      html +=
        '<tr>' +
          '<td style="word-break:break-all;max-width:300px">' + escHtml(s.url) + '</td>' +
          '<td>' + s.distance + '</td>' +
          '<td>' + s.max_concurrent_requests + '</td>' +
          '<td>' + s.inflight + '</td>' +
          '<td><span class="status-dot ' + statusClass + '" style="display:inline-block;vertical-align:middle;margin-right:0.375rem"></span>' +
            (s.healthy ? 'Healthy' : 'Unhealthy') + '</td>' +
          '<td style="max-width:200px">' + modelsHtml + '</td>' +
          '<td>' +
            '<button class="btn btn-primary btn-sm" onclick="window.__editServer(\'' +
              encodeURIComponent(s.url) + '\',' + s.distance + ',' + s.max_concurrent_requests + ')">Edit</button>' +
            '<button class="btn btn-danger btn-sm" onclick="window.__deleteServer(\'' +
              encodeURIComponent(s.url) + '\')">Delete</button>' +
          '</td>' +
        '</tr>';
    }
    tbody.innerHTML = html;
  }

  // ---------------------------------------------------------------------------
  // Config
  // ---------------------------------------------------------------------------

  function loadConfig() {
    fetch('/admin/config')
      .then(function (res) {
        if (!res.ok) throw new Error();
        return res.json();
      })
      .then(function (cfg) {
        document.getElementById('config-pre').textContent = JSON.stringify(cfg, null, 2);
      })
      .catch(function () {
        showToast('Failed to load config', 'error');
      });
  }

  function openConfigEditor() {
    var pre = document.getElementById('config-pre');
    var textarea = document.getElementById('config-textarea');
    textarea.value = pre.textContent;
    document.getElementById('config-editor').classList.remove('hidden');
    document.getElementById('btn-edit-config').disabled = true;
    textarea.focus();
  }

  function closeConfigEditor() {
    document.getElementById('config-editor').classList.add('hidden');
    document.getElementById('btn-edit-config').disabled = false;
  }

  function saveConfig() {
    var textarea = document.getElementById('config-textarea');
    var raw = textarea.value;

    try {
      JSON.parse(raw);
    } catch (e) {
      showToast('Invalid JSON: ' + e.message, 'error');
      return;
    }

    fetch('/admin/config', {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: raw
    })
      .then(function (res) {
        if (!res.ok) {
          return res.json().then(function (data) {
            throw new Error(data.error && data.error.message ? data.error.message : 'Request failed');
          });
        }
        return res.json();
      })
      .then(function (cfg) {
        document.getElementById('config-pre').textContent = JSON.stringify(cfg, null, 2);
        closeConfigEditor();
        showToast('Config updated', 'success');
      })
      .catch(function (err) {
        showToast('Failed to save config: ' + err.message, 'error');
      });
  }

  // ---------------------------------------------------------------------------
  // OpenCode Config
  // ---------------------------------------------------------------------------

  function loadOpenCodeConfig() {
    var baseUrl = window.location.origin;
    fetch('/admin/opencode-config?base_url=' + encodeURIComponent(baseUrl))
      .then(function (res) {
        if (!res.ok) throw new Error('Status ' + res.status);
        return res.text();
      })
      .then(function (text) {
        document.getElementById('opencode-pre').textContent = text;
      })
      .catch(function () {
        showToast('Failed to load opencode config', 'error');
      });
  }

  function copyOpenCodeConfig() {
    var pre = document.getElementById('opencode-pre');
    var text = pre.textContent;
    if (!text) {
      showToast('No config loaded yet', 'error');
      return;
    }
    if (navigator.clipboard && navigator.clipboard.writeText) {
      navigator.clipboard.writeText(text).then(function () {
        showToast('Config copied to clipboard', 'success');
      }).catch(function () {
        showToast('Failed to copy', 'error');
      });
    } else {
      // Fallback: select text
      var range = document.createRange();
      range.selectNodeContents(pre);
      var sel = window.getSelection();
      sel.removeAllRanges();
      sel.addRange(range);
      document.execCommand('copy');
      sel.removeAllRanges();
      showToast('Config copied to clipboard', 'success');
    }
  }

  // ---------------------------------------------------------------------------
  // Status
  // ---------------------------------------------------------------------------

  function loadStatus() {
    fetch('/admin/status')
      .then(function (res) {
        if (!res.ok) throw new Error();
        return res.json();
      })
      .then(function (data) {
        document.getElementById('status-pre').textContent = JSON.stringify(data, null, 2);
      })
      .catch(function () {
        showToast('Failed to load status', 'error');
      });
  }

  // ---------------------------------------------------------------------------
  // Chat
  // ---------------------------------------------------------------------------

  function loadModels() {
    fetch('/v1/models')
      .then(function (res) { return res.json(); })
      .then(function (data) {
        var select = document.getElementById('chat-model-select');
        select.innerHTML = '<option value="">Select a model...</option>';
        if (data && data.data) {
          data.data.forEach(function (m) {
            var opt = document.createElement('option');
            opt.value = m.id;
            opt.textContent = m.id;
            select.appendChild(opt);
          });
        }
        updateSendButton();
      })
      .catch(function () {
        showToast('Failed to load models', 'error');
      });
  }

  function updateSendButton() {
    var select = document.getElementById('chat-model-select');
    var btn = document.getElementById('btn-chat-send');
    btn.disabled = !select.value;
  }

  function appendMessage(role, content, streaming) {
    var container = document.getElementById('chat-messages');
    var div = document.createElement('div');
    div.className = 'chat-message ' + role;
    if (streaming) div.classList.add('streaming');

    var inner = document.createElement('div');
    inner.className = 'chat-message-bubble';
    inner.textContent = content;
    div.appendChild(inner);

    container.appendChild(div);
    container.scrollTop = container.scrollHeight;
    return inner;
  }

  function disableChatControls(disabled) {
    chatBusy = disabled;
    document.getElementById('btn-chat-send').disabled = disabled;
    document.getElementById('chat-model-select').disabled = disabled;
    document.getElementById('chat-message-input').disabled = disabled;
  }

  function finishChat(startTime, firstTokenTime, content, status) {
    disableChatControls(false);

    var totalMs = Date.now() - startTime;
    var ttftMs = firstTokenTime ? firstTokenTime - startTime : 0;

    var tokenEstimate = Math.round(content.length / 4);

    document.getElementById('metric-model').textContent =
      document.getElementById('chat-model-select').value;
    document.getElementById('metric-ttft').textContent = ttftMs + ' ms';
    document.getElementById('metric-total').textContent = totalMs + ' ms';
    document.getElementById('metric-tokens').textContent = '~' + tokenEstimate;

    var statusEl = document.getElementById('metric-status');
    statusEl.textContent = status;
    statusEl.style.color = status === 'success' ? 'var(--green)' : 'var(--red)';

    document.getElementById('metric-backend').textContent = 'via bridge';
    document.getElementById('chat-metrics').classList.remove('hidden');

    var msg = document.querySelector('.chat-message.streaming');
    if (msg) msg.classList.remove('streaming');
  }

  function sendChatMessage() {
    var model = document.getElementById('chat-model-select').value;
    var input = document.getElementById('chat-message-input');
    var message = input.value.trim();

    if (!model || !message) return;

    appendMessage('user', message);
    input.value = '';

    disableChatControls(true);

    var startTime = Date.now();
    var firstTokenTime = null;
    var fullContent = '';
    var assistantMsgEl = appendMessage('assistant', '', true);

    var body = JSON.stringify({
      model: model,
      messages: [{ role: 'user', content: message }],
      stream: true
    });

    fetch('/v1/chat/completions', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: body
    })
      .then(function (res) {
        if (!res.ok) {
          throw new Error('HTTP ' + res.status);
        }

        var reader = res.body.getReader();
        var decoder = new TextDecoder();
        var buffer = '';

        function read() {
          return reader.read().then(function (result) {
            if (result.done) {
              finishChat(startTime, firstTokenTime, fullContent, 'success');
              return;
            }
            buffer += decoder.decode(result.value, { stream: true });
            var lines = buffer.split('\n');
            buffer = lines.pop();

            for (var i = 0; i < lines.length; i++) {
              var line = lines[i].trim();
              if (line.startsWith('data: ')) {
                var data = line.slice(6);
                if (data === '[DONE]') continue;
                try {
                  var parsed = JSON.parse(data);
                  var content = parsed.choices && parsed.choices[0] &&
                    parsed.choices[0].delta && parsed.choices[0].delta.content;
                  if (content) {
                    if (!firstTokenTime) firstTokenTime = Date.now();
                    fullContent += content;
                    assistantMsgEl.textContent = fullContent;
                  }
                } catch (e) { /* skip invalid JSON */ }
              }
            }
            return read();
          });
        }
        return read();
      })
      .catch(function (err) {
        if (err.name === 'AbortError' || err.name === 'TypeError') {
          showToast('Connection error', 'error');
        } else {
          showToast('Failed: ' + err.message, 'error');
        }
        finishChat(startTime, firstTokenTime, fullContent, 'error: ' + err.message);
      });
  }

  // ---------------------------------------------------------------------------
  // Modal (Add / Edit Server)
  // ---------------------------------------------------------------------------

  function openAddModal() {
    modalMode = 'add';
    modalOriginalUrl = '';
    document.getElementById('modal-title').textContent = 'Add Server';
    document.getElementById('modal-url').value = '';
    document.getElementById('modal-distance').value = '1';
    document.getElementById('modal-max-concurrent').value = '5';
    document.getElementById('modal-overlay').classList.remove('hidden');
    document.getElementById('modal-url').focus();
  }

  function closeModal() {
    document.getElementById('modal-overlay').classList.add('hidden');
  }

  function handleModalSubmit(e) {
    e.preventDefault();
    var url = document.getElementById('modal-url').value.trim();
    var distance = parseInt(document.getElementById('modal-distance').value, 10);
    var maxConcurrent = parseInt(document.getElementById('modal-max-concurrent').value, 10);

    if (!url) {
      showToast('URL is required', 'error');
      return;
    }
    if (isNaN(distance) || distance < 1 || distance > 10) {
      showToast('Distance must be between 1 and 10', 'error');
      return;
    }
    if (isNaN(maxConcurrent) || maxConcurrent < 1) {
      showToast('Max Concurrent Requests must be at least 1', 'error');
      return;
    }

    if (modalMode === 'add') {
      fetch('/admin/servers', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          url: url,
          distance: distance,
          max_concurrent_requests: maxConcurrent
        })
      })
        .then(function (res) {
          if (res.status === 201) {
            showToast('Server added', 'success');
            closeModal();
            loadDashboard();
            loadServers();
          } else {
            return res.json().then(function (data) {
              throw new Error(
                data.error && data.error.message ? data.error.message : 'Failed to add server'
              );
            });
          }
        })
        .catch(function (err) {
          showToast(err.message, 'error');
        });
    } else if (modalMode === 'edit') {
      var body = {
        distance: distance,
        max_concurrent_requests: maxConcurrent
      };
      if (url !== modalOriginalUrl) {
        body.url = url;
      }

      fetch('/admin/servers/' + encodeURIComponent(modalOriginalUrl), {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(body)
      })
        .then(function (res) {
          if (res.ok) {
            showToast('Server updated', 'success');
            closeModal();
            loadDashboard();
            loadServers();
          } else {
            return res.json().then(function (data) {
              throw new Error(
                data.error && data.error.message ? data.error.message : 'Failed to update server'
              );
            });
          }
        })
        .catch(function (err) {
          showToast(err.message, 'error');
        });
    }
  }

  // ---------------------------------------------------------------------------
  // Delete Server (exposed globally for onclick)
  // ---------------------------------------------------------------------------

  function deleteServer(encodedUrl) {
    var url = decodeURIComponent(encodedUrl);
    if (!confirm('Delete server "' + url + '"?')) return;

    fetch('/admin/servers/' + encodeURIComponent(url), { method: 'DELETE' })
      .then(function (res) {
        if (res.status === 204) {
          showToast('Server deleted', 'success');
          loadDashboard();
          if (currentSection === 'servers') loadServers();
        } else {
          return res.json().then(function (data) {
            throw new Error(
              data.error && data.error.message ? data.error.message : 'Failed to delete server'
            );
          });
        }
      })
      .catch(function (err) {
        showToast(err.message, 'error');
      });
  }

  function editServer(encodedUrl, distance, maxConcurrent) {
    var url = decodeURIComponent(encodedUrl);
    modalMode = 'edit';
    modalOriginalUrl = url;
    document.getElementById('modal-title').textContent = 'Edit Server';
    document.getElementById('modal-url').value = url;
    document.getElementById('modal-distance').value = distance;
    document.getElementById('modal-max-concurrent').value = maxConcurrent;
    document.getElementById('modal-overlay').classList.remove('hidden');
    document.getElementById('modal-url').focus();
  }

  window.__deleteServer = deleteServer;
  window.__editServer = editServer;
})();

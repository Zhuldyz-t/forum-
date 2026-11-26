(function () {
  'use strict';

  var shell = document.querySelector('[data-chat-shell]');
  if (!shell) return;
  var me = Number(shell.getAttribute('data-me'));
  var csrf = shell.getAttribute('data-csrf');
  var list = document.querySelector('[data-chat-list]');
  var chat = document.querySelector('[data-chat]');

  var getJSON = function (url) {
    return fetch(url, { credentials: 'same-origin', headers: { Accept: 'application/json' } })
      .then(function (r) { return r.json(); });
  };
  var postJSON = function (url, fields) {
    var form = new FormData();
    Object.keys(fields).forEach(function (k) { form.append(k, fields[k]); });
    form.append('csrf_token', csrf);
    return fetch(url, { method: 'POST', body: form, credentials: 'same-origin', headers: { Accept: 'application/json' } })
      .then(function (r) { return r.json(); });
  };

  var CHECK = '<svg viewBox="0 0 16 16" width="16" height="16" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="m2.5 8.5 3 3 7-7"/></svg>';
  var DOUBLE = '<svg viewBox="0 0 20 16" width="20" height="16" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="m1.5 8.5 3 3 7-7"/><path d="m8.5 11.5 7-7"/></svg>';

  var pad = function (n) { return (n < 10 ? '0' : '') + n; };
  var hhmm = function (d) { return pad(d.getHours()) + ':' + pad(d.getMinutes()); };
  var dayLabel = function (d) {
    var today = new Date(); today.setHours(0, 0, 0, 0);
    var day = new Date(d); day.setHours(0, 0, 0, 0);
    var diff = Math.round((today - day) / 86400000);
    if (diff === 0) return 'Сегодня';
    if (diff === 1) return 'Вчера';
    return day.toLocaleDateString('ru-RU', { day: 'numeric', month: 'long', year: day.getFullYear() === today.getFullYear() ? undefined : 'numeric' });
  };

  // ─── Dialog list ───────────────────────────────────────────
  var refreshList = function () {
    var active = chat ? chat.getAttribute('data-conv') : 0;
    getJSON('/messages/conversations?active=' + active).then(function (d) {
      if (d && d.html) list.innerHTML = d.html;
    }).catch(function () {});
  };
  var updateListItem = function (m) {
    var item = list.querySelector('[data-conv-item="' + m.conversation_id + '"]');
    if (!item) { refreshList(); return; }
    item.querySelector('[data-conv-last]').textContent = (m.sender_id === me ? 'Вы: ' : '') + m.text;
    item.querySelector('[data-conv-time]').textContent = hhmm(new Date(m.created_at));
    var isOpen = chat && Number(chat.getAttribute('data-conv')) === m.conversation_id;
    if (m.sender_id !== me && !isOpen) {
      var badge = item.querySelector('[data-conv-unread]');
      badge.textContent = String(Number(badge.textContent || 0) + 1);
      badge.hidden = false;
    }
    list.insertBefore(item, list.firstChild);
  };

  // ─── Presence (both list and chat header) ──────────────────
  var statusEl = chat && chat.querySelector('[data-chat-status]');
  var baseStatus = statusEl ? statusEl.textContent : '';
  var typingTimer = null;
  var onPresence = function (ev) {
    document.querySelectorAll('[data-presence-dot="' + ev.user_id + '"]').forEach(function (dot) {
      dot.classList.toggle('is-online', ev.online);
    });
    if (chat && Number(chat.getAttribute('data-partner')) === ev.user_id) {
      baseStatus = ev.status;
      if (!typingTimer) statusEl.textContent = baseStatus;
    }
  };

  if (!chat) {
    document.addEventListener('forum:ws', function (e) {
      var ev = e.detail;
      if (ev.type === 'message') updateListItem(ev.message);
      if (ev.type === 'presence') onPresence(ev);
    });
    return;
  }

  // ─── Open chat ─────────────────────────────────────────────
  var conv = Number(chat.getAttribute('data-conv'));
  var partner = Number(chat.getAttribute('data-partner'));
  var box = chat.querySelector('[data-chat-messages]');
  var loader = chat.querySelector('[data-chat-loader]');
  var empty = chat.querySelector('[data-chat-empty]');
  var form = chat.querySelector('[data-chat-form]');
  var input = chat.querySelector('[data-chat-input]');
  var errorEl = chat.querySelector('[data-chat-error]');
  var hasMore = false, loading = false;
  var oldestId = 0, newestId = 0;

  var ticks = function (m) {
    if (m.is_read) return '<span class="msg-ticks is-read" title="Прочитано">' + DOUBLE + '</span>';
    if (m.is_delivered) return '<span class="msg-ticks" title="Доставлено">' + CHECK + '</span>';
    return '<span class="msg-ticks is-pending" title="Отправлено">' + CHECK + '</span>';
  };

  var renderMessage = function (m) {
    var mine = m.sender_id === me;
    var el = document.createElement('div');
    el.className = 'msg ' + (mine ? 'msg-out' : 'msg-in');
    el.setAttribute('data-msg', m.id);
    el.setAttribute('data-day', new Date(m.created_at).toDateString());
    var bubble = document.createElement('div');
    bubble.className = 'msg-bubble';
    var text = document.createElement('span');
    text.className = 'msg-text';
    text.textContent = m.text; // always text: never parsed as HTML
    var meta = document.createElement('span');
    meta.className = 'msg-meta';
    var time = document.createElement('time');
    time.textContent = hhmm(new Date(m.created_at));
    time.title = new Date(m.created_at).toLocaleString('ru-RU');
    meta.appendChild(time);
    if (mine) meta.insertAdjacentHTML('beforeend', ticks(m));
    bubble.appendChild(text);
    bubble.appendChild(meta);
    el.appendChild(bubble);
    el._msg = m;
    return el;
  };

  // Day separators are recomputed after every insert (prepend or append).
  var relabelDays = function () {
    box.querySelectorAll('.msg-day').forEach(function (d) { d.remove(); });
    var prev = '';
    box.querySelectorAll('[data-msg]').forEach(function (el) {
      var day = el.getAttribute('data-day');
      if (day !== prev) {
        var sep = document.createElement('div');
        sep.className = 'msg-day';
        sep.textContent = dayLabel(new Date(day));
        box.insertBefore(sep, el);
        prev = day;
      }
    });
  };

  var nearBottom = function () { return box.scrollHeight - box.scrollTop - box.clientHeight < 120; };
  var scrollToBottom = function () { box.scrollTop = box.scrollHeight; };

  var append = function (m) {
    if (box.querySelector('[data-msg="' + m.id + '"]')) return; // already shown (own tab)
    var stick = nearBottom() || m.sender_id === me;
    box.appendChild(renderMessage(m));
    newestId = Math.max(newestId, m.id);
    if (!oldestId) oldestId = m.id;
    empty.hidden = true;
    relabelDays();
    if (stick) scrollToBottom();
  };

  var markRead = function () {
    if (!conv || document.visibilityState !== 'visible') return;
    postJSON('/messages/read', { conversation_id: conv }).catch(function () {});
    var item = list.querySelector('[data-conv-item="' + conv + '"] [data-conv-unread]');
    if (item) { item.hidden = true; item.textContent = '0'; }
  };

  var loadOlder = function () {
    if (!conv || loading || (!hasMore && oldestId)) return;
    loading = true;
    loader.hidden = false;
    getJSON('/messages/history?c=' + conv + (oldestId ? '&before=' + oldestId : ''))
      .then(function (d) {
        if (!d || !d.messages) return;
        var first = !oldestId;
        var prevHeight = box.scrollHeight, prevTop = box.scrollTop;
        var anchor = loader.nextSibling;
        d.messages.forEach(function (m) {
          if (box.querySelector('[data-msg="' + m.id + '"]')) return;
          box.insertBefore(renderMessage(m), anchor);
          newestId = Math.max(newestId, m.id);
        });
        if (d.messages.length) oldestId = d.messages[0].id;
        hasMore = d.has_more;
        empty.hidden = box.querySelector('[data-msg]') !== null;
        relabelDays();
        if (first) scrollToBottom();
        else box.scrollTop = box.scrollHeight - prevHeight + prevTop; // keep the view still
      })
      .catch(function () {})
      .then(function () { loading = false; loader.hidden = true; });
  };

  // Older messages appear when scrolling to the top, 30 at a time.
  box.addEventListener('scroll', function () {
    if (box.scrollTop < 80 && hasMore) loadOlder();
  });

  // After a reconnect, fetch whatever arrived while the socket was down.
  var catchUp = function () {
    if (!conv) return;
    getJSON('/messages/history?c=' + conv).then(function (d) {
      if (!d || !d.messages) return;
      d.messages.forEach(function (m) {
        var el = box.querySelector('[data-msg="' + m.id + '"]');
        if (el) updateTicks(el, m);
        else if (m.id > newestId) append(m);
      });
      markRead();
    }).catch(function () {});
  };

  var updateTicks = function (el, m) {
    var t = el.querySelector('.msg-ticks');
    if (!t) return;
    t.outerHTML = ticks(m);
    el._msg = m;
  };

  // ─── Sending ───────────────────────────────────────────────
  var autosize = function () {
    input.style.height = 'auto';
    input.style.height = Math.min(input.scrollHeight, 140) + 'px';
  };
  var lastTyping = 0;
  input.addEventListener('input', function () {
    autosize();
    var now = Date.now();
    if (conv && input.value.trim() && now - lastTyping > 2500) {
      lastTyping = now;
      window.forumWS.send({ type: 'typing', conversation_id: conv });
    }
  });
  input.addEventListener('keydown', function (e) {
    if (e.key === 'Enter' && !e.shiftKey && !e.isComposing) {
      e.preventDefault();
      form.requestSubmit();
    }
  });
  form.addEventListener('submit', function (e) {
    e.preventDefault();
    var text = input.value.trim();
    if (!text) return;
    if (text.length > 2000) {
      errorEl.textContent = 'Сообщение длиннее 2000 символов';
      errorEl.hidden = false;
      return;
    }
    errorEl.hidden = true;
    var btn = form.querySelector('button');
    btn.disabled = true;
    var fields = { text: text };
    if (conv) fields.conversation_id = conv; else fields.to = partner;
    postJSON('/messages/send', fields).then(function (d) {
      if (!d || d.error) {
        errorEl.textContent = (d && d.error) || 'Не удалось отправить';
        errorEl.hidden = false;
        return;
      }
      var m = d.message;
      if (!conv) {
        // The first message created the conversation.
        conv = m.conversation_id;
        chat.setAttribute('data-conv', conv);
        history.replaceState(null, '', '/messages?c=' + conv);
      }
      input.value = '';
      autosize();
      append(m);
      updateListItem(m);
    }).catch(function () {
      errorEl.textContent = 'Нет связи с сервером. Сообщение не отправлено.';
      errorEl.hidden = false;
    }).then(function () { btn.disabled = false; input.focus(); });
  });

  // ─── Live events ───────────────────────────────────────────
  document.addEventListener('forum:ws', function (e) {
    var ev = e.detail;
    switch (ev.type) {
      case 'message':
        if (ev.message.conversation_id === conv) {
          append(ev.message);
          if (ev.message.sender_id !== me) {
            clearTimeout(typingTimer);
            typingTimer = null;
            statusEl.textContent = baseStatus;
            statusEl.classList.remove('is-typing');
            markRead();
          }
        }
        updateListItem(ev.message);
        break;
      case 'typing':
        if (ev.conversation_id !== conv) return;
        statusEl.textContent = 'печатает…';
        statusEl.classList.add('is-typing');
        clearTimeout(typingTimer);
        typingTimer = setTimeout(function () {
          typingTimer = null;
          statusEl.textContent = baseStatus;
          statusEl.classList.remove('is-typing');
        }, 3500);
        break;
      case 'read':
        if (ev.conversation_id !== conv) return;
        box.querySelectorAll('.msg-out[data-msg]').forEach(function (el) {
          if (Number(el.getAttribute('data-msg')) <= ev.up_to) {
            var m = el._msg; m.is_read = true; m.is_delivered = true;
            updateTicks(el, m);
          }
        });
        break;
      case 'delivered':
        (ev.ids || []).forEach(function (id) {
          var el = box.querySelector('.msg-out[data-msg="' + id + '"]');
          if (el && !el._msg.is_read) { el._msg.is_delivered = true; updateTicks(el, el._msg); }
        });
        break;
      case 'presence':
        onPresence(ev);
        break;
    }
  });
  document.addEventListener('forum:ws-open', function () {
    if (newestId) catchUp();
    refreshList();
  });
  document.addEventListener('visibilitychange', markRead);

  if (conv) {
    loadOlder();
    markRead();
  }
  input.focus();
})();

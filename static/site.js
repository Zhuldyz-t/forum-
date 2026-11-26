(function () {
  'use strict';

  // ─── Theme ─────────────────────────────────────────────────
  // The initial theme (dark by default) is set in <head> by theme-init, so
  // the page never flashes white.
  var root = document.documentElement;
  document.querySelectorAll('[data-theme-toggle]').forEach(function (btn) {
    btn.addEventListener('click', function () {
      var next = root.getAttribute('data-theme') === 'dark' ? 'light' : 'dark';
      // Animate colors only while switching, so other transitions stay snappy.
      root.classList.add('theme-switching');
      root.setAttribute('data-theme', next);
      setTimeout(function () { root.classList.remove('theme-switching'); }, 250);
      try { localStorage.setItem('theme', next); } catch (e) { /* private mode */ }
    });
  });

  // JSON POST helper for the fetch endpoints (CSRF token goes in the form).
  var postJSON = function (url, fields) {
    var form = new FormData();
    Object.keys(fields).forEach(function (k) { form.append(k, fields[k]); });
    return fetch(url, {
      method: 'POST', body: form, credentials: 'same-origin',
      headers: { Accept: 'application/json' }
    }).then(function (res) {
      if (res.status === 401) { window.location.href = '/login'; return null; }
      return res.json();
    });
  };
  var getJSON = function (url) {
    return fetch(url, { credentials: 'same-origin', headers: { Accept: 'application/json' } })
      .then(function (res) { return res.ok ? res.json() : null; });
  };

  // ─── Follow / unfollow ─────────────────────────────────────
  document.addEventListener('click', function (e) {
    var btn = e.target.closest('button[data-follow]');
    if (!btn || btn.disabled) return;
    var id = btn.getAttribute('data-follow');
    var following = btn.classList.contains('is-following');
    btn.disabled = true;
    postJSON(following ? '/unfollow' : '/follow', { user_id: id, csrf_token: btn.getAttribute('data-csrf') })
      .then(function (data) {
        if (!data || data.error) return;
        // The same author can appear several times (widget, list, profile).
        document.querySelectorAll('button[data-follow="' + id + '"]').forEach(function (b) {
          b.classList.toggle('is-following', data.following);
          b.setAttribute('aria-pressed', data.following ? 'true' : 'false');
        });
        var stats = document.querySelector('[data-follow-stats="' + id + '"] [data-followers-text]');
        if (stats) stats.textContent = data.followersText;
      })
      .catch(function () { /* leave the button as it was */ })
      .then(function () { btn.disabled = false; });
  });

  // ─── Followers / following modal ───────────────────────────
  var followModal = document.getElementById('follow-modal');
  if (followModal) {
    var body = followModal.querySelector('[data-follow-list-body]');
    var search = followModal.querySelector('[data-follow-search]');
    var noMatch = followModal.querySelector('[data-follow-nomatch]');
    var opener = null;
    var closeModal = function () {
      followModal.hidden = true;
      document.body.style.overflow = '';
      if (opener) opener.focus();
    };
    var filter = function () {
      var q = search.value.trim().toLowerCase();
      var shown = 0;
      body.querySelectorAll('.follow-row').forEach(function (row) {
        var hit = row.getAttribute('data-name').toLowerCase().indexOf(q) !== -1;
        row.hidden = !hit;
        if (hit) shown++;
      });
      noMatch.hidden = !(q && shown === 0 && body.querySelector('.follow-row'));
    };
    document.addEventListener('click', function (e) {
      var trigger = e.target.closest('[data-follow-list]');
      if (!trigger) return;
      opener = trigger;
      followModal.querySelector('.modal-title').textContent = trigger.getAttribute('data-title');
      search.value = '';
      noMatch.hidden = true;
      body.innerHTML = '<li class="follow-empty">Загрузка…</li>';
      followModal.hidden = false;
      document.body.style.overflow = 'hidden';
      search.focus();
      getJSON(trigger.getAttribute('data-follow-list')).then(function (data) {
        body.innerHTML = data && data.html ? data.html : '<li class="follow-empty">Не удалось загрузить список</li>';
      });
    });
    search.addEventListener('input', filter);
    followModal.querySelector('[data-follow-modal-close]').addEventListener('click', closeModal);
    followModal.addEventListener('click', function (e) { if (e.target === followModal) closeModal(); });
    document.addEventListener('keydown', function (e) {
      if (e.key === 'Escape' && !followModal.hidden) closeModal();
    });
  }

  // ─── Notifications bell ────────────────────────────────────
  var notif = document.querySelector('[data-notif]');
  if (notif) {
    var badge = notif.querySelector('[data-notif-badge]');
    var toggle = notif.querySelector('[data-notif-toggle]');
    var menu = notif.querySelector('[data-notif-menu]');
    var list = notif.querySelector('[data-notif-list]');
    var csrf = notif.getAttribute('data-csrf');

    var setCount = function (n) {
      badge.textContent = n > 99 ? '99+' : String(n);
      badge.hidden = !n;
      toggle.setAttribute('aria-label', n ? 'Уведомления: ' + n + ' новых' : 'Уведомления');
    };
    var refresh = function () {
      getJSON('/notifications/count').then(function (d) { if (d) setCount(d.count); }).catch(function () {});
    };
    refresh();
    setInterval(refresh, 60000);
    document.addEventListener('forum:ws', function (e) { if (e.detail.type === 'achievement') refresh(); });

    var markRead = function () {
      return postJSON('/notifications/read', { csrf_token: csrf }).catch(function () {});
    };
    var setMenu = function (open) {
      menu.hidden = !open;
      toggle.setAttribute('aria-expanded', open ? 'true' : 'false');
    };
    toggle.addEventListener('click', function (e) {
      e.stopPropagation();
      if (!menu.hidden) { setMenu(false); return; }
      setMenu(true);
      list.innerHTML = '<p class="notif-empty">Загрузка…</p>';
      getJSON('/notifications').then(function (d) {
        list.innerHTML = d && d.html ? d.html : '<p class="notif-empty">Не удалось загрузить уведомления</p>';
        // Opening the list counts as seeing it: reset the badge. Unread items
        // stay highlighted in this list until «Прочитать все».
        if (!badge.hidden) markRead().then(function () { setCount(0); });
      });
    });
    notif.querySelector('[data-notif-readall]').addEventListener('click', function () {
      markRead().then(function () {
        setCount(0);
        list.querySelectorAll('.is-unread').forEach(function (el) { el.classList.remove('is-unread'); });
      });
    });
    document.addEventListener('click', function (e) {
      if (!menu.hidden && !notif.contains(e.target)) setMenu(false);
    });
    document.addEventListener('keydown', function (e) {
      if (e.key === 'Escape' && !menu.hidden) { setMenu(false); toggle.focus(); }
    });
  }

  // ─── Back to top ───────────────────────────────────────────
  var toTop = document.querySelector('[data-to-top]');
  if (toTop) {
    var ticking = false;
    window.addEventListener('scroll', function () {
      if (ticking) return;
      ticking = true;
      requestAnimationFrame(function () {
        toTop.hidden = window.scrollY < 600;
        ticking = false;
      });
    }, { passive: true });
    toTop.addEventListener('click', function () {
      window.scrollTo({ top: 0, behavior: 'smooth' });
    });
  }

  // ─── Likes / dislikes without reload ───────────────────────
  // Forms still work without JS (the server redirects back).
  document.addEventListener('submit', function (e) {
    var form = e.target.closest('form[data-react-form]');
    if (!form) return;
    e.preventDefault();
    var box = form.closest('[data-reactions]');
    if (box.classList.contains('is-busy')) return;
    box.classList.add('is-busy');

    fetch(form.action, {
      method: 'POST',
      body: new FormData(form),
      credentials: 'same-origin',
      headers: { Accept: 'application/json' }
    })
      .then(function (res) {
        if (res.status === 401) { window.location.href = '/login'; return null; }
        return res.json();
      })
      .then(function (data) {
        if (!data || data.error) return;
        var like = box.querySelector('.react-like');
        var dislike = box.querySelector('.react-dislike');
        like.querySelector('[data-count]').textContent = data.likes;
        dislike.querySelector('[data-count]').textContent = data.dislikes;
        like.classList.toggle('is-active', data.mine === 1);
        dislike.classList.toggle('is-active', data.mine === -1);
        like.setAttribute('aria-pressed', data.mine === 1 ? 'true' : 'false');
        dislike.setAttribute('aria-pressed', data.mine === -1 ? 'true' : 'false');
      })
      .catch(function () { /* network error: leave the counters as they were */ })
      .then(function () { box.classList.remove('is-busy'); });
  });

  // ─── Feed: live search, «Показать ещё», skeletons ──────────
  var feed = document.querySelector('[data-feed]');
  if (feed) {
    var input = document.querySelector('[data-feed-input]');
    var clearBtn = document.querySelector('[data-feed-clear]');
    var moreBtn = document.querySelector('[data-feed-more]');
    var state = {
      category: feed.getAttribute('data-category') || '',
      sort: feed.getAttribute('data-sort') || 'new',
      filter: feed.getAttribute('data-filter') || '',
      tab: feed.getAttribute('data-tab') || 'all',
      q: feed.getAttribute('data-q') || '',
      page: 1
    };
    var requestId = 0;

    var params = function (page) {
      var p = new URLSearchParams();
      if (state.category) p.set('category', state.category);
      if (state.q) p.set('q', state.q);
      if (state.sort && state.sort !== 'new') p.set('sort', state.sort);
      if (state.filter) p.set('filter', state.filter);
      if (state.tab === 'following') p.set('feed', 'following');
      if (page) p.set('page', page);
      return p;
    };

    var skeletons = function (n) {
      var html = '';
      for (var i = 0; i < n; i++) {
        html += '<div class="card skeleton" aria-hidden="true">' +
          '<div class="sk-row"><span class="sk sk-avatar"></span><span class="sk sk-line sk-w30"></span></div>' +
          '<span class="sk sk-line sk-title"></span>' +
          '<span class="sk sk-line"></span><span class="sk sk-line sk-w80"></span>' +
          '</div>';
      }
      var wrap = document.createElement('div');
      wrap.className = 'skeletons';
      wrap.innerHTML = html;
      return wrap;
    };

    var showFeedError = function (msg) {
      var p = document.createElement('p');
      p.className = 'feed-error';
      p.textContent = msg;
      feed.appendChild(p);
    };

    var load = function (page, replace) {
      var id = ++requestId;
      var sk = skeletons(replace ? 3 : 2);
      if (replace) feed.innerHTML = '';
      feed.appendChild(sk);
      feed.setAttribute('aria-busy', 'true');
      moreBtn.hidden = true;

      fetch('/feed?' + params(page).toString(), {
        credentials: 'same-origin',
        headers: { Accept: 'application/json' }
      })
        .then(function (res) { return res.json(); })
        .then(function (data) {
          if (id !== requestId) return; // a newer request superseded this one
          sk.remove();
          if (data.error) { showFeedError(data.error); return; }
          feed.insertAdjacentHTML('beforeend', data.html);
          state.page = page;
          moreBtn.hidden = !data.hasMore;
        })
        .catch(function () {
          if (id !== requestId) return;
          sk.remove();
          showFeedError('Не удалось загрузить посты. Проверьте соединение.');
          moreBtn.hidden = false;
        })
        .then(function () {
          if (id === requestId) feed.removeAttribute('aria-busy');
        });
    };

    // Keep category and sort links in sync with the current search.
    var syncLinks = function () {
      document.querySelectorAll('.cat-link, .segment, .filter-chip, [data-feed-tab]').forEach(function (a) {
        var url = new URL(a.href, window.location.origin);
        if (state.q) url.searchParams.set('q', state.q); else url.searchParams.delete('q');
        a.href = url.pathname + (url.search || '');
      });
      var qs = params(0).toString();
      window.history.replaceState(null, '', '/' + (qs ? '?' + qs : ''));
    };

    var search = function () {
      var q = input.value.trim();
      clearBtn.hidden = input.value === '';
      if (q === state.q) return;
      state.q = q;
      syncLinks();
      load(1, true);
    };

    if (moreBtn) {
      moreBtn.addEventListener('click', function () { load(state.page + 1, false); });
    }
    if (input) {
      var timer = null;
      input.addEventListener('input', function () {
        clearBtn.hidden = input.value === '';
        clearTimeout(timer);
        timer = setTimeout(search, 300);
      });
      input.form.addEventListener('submit', function (e) {
        e.preventDefault();
        clearTimeout(timer);
        search();
      });
      clearBtn.addEventListener('click', function () {
        input.value = '';
        clearTimeout(timer);
        search();
        input.focus();
      });
    }
  }

  // ─── WebSocket: one connection per tab for logged-in users ─
  // Events are re-dispatched on document as «forum:ws» so page scripts
  // (messages.js) can listen; «forum:ws-open» fires on every (re)connect.
  var wsMarker = document.querySelector('[data-ws-enabled]');
  window.forumWS = { send: function () { return false; } };
  if (wsMarker && window.WebSocket) {
    var socket = null;
    var connect = function () {
      socket = new WebSocket((location.protocol === 'https:' ? 'wss://' : 'ws://') + location.host + '/ws');
      socket.onopen = function () { document.dispatchEvent(new CustomEvent('forum:ws-open')); };
      socket.onmessage = function (e) {
        var ev;
        try { ev = JSON.parse(e.data); } catch (err) { return; }
        document.dispatchEvent(new CustomEvent('forum:ws', { detail: ev }));
      };
      // Reconnect every 3 seconds after the connection drops.
      socket.onclose = function () { setTimeout(connect, 3000); };
    };
    window.forumWS.send = function (obj) {
      if (!socket || socket.readyState !== 1) return false;
      socket.send(JSON.stringify(obj));
      return true;
    };
    connect();
  }

  // ─── Unread messages badge ─────────────────────────────────
  var msgBadge = document.querySelector('[data-msg-badge]');
  if (msgBadge) {
    var setMsgCount = function (n) {
      msgBadge.textContent = n > 99 ? '99+' : String(n);
      msgBadge.hidden = !n;
      msgBadge.parentNode.setAttribute('aria-label', n ? 'Сообщения: ' + n + ' непрочитанных' : 'Сообщения');
    };
    var refreshMsgs = function () {
      getJSON('/messages/unread').then(function (d) { if (d) setMsgCount(d.count); }).catch(function () {});
    };
    refreshMsgs();
    document.addEventListener('forum:ws-open', refreshMsgs);
    document.addEventListener('forum:ws', function (e) {
      if (e.detail.type === 'unread_messages') setMsgCount(e.detail.count);
    });
  }

  // ─── «Новое достижение» toasts ─────────────────────────────
  // Pending toasts are fetched on every page load (the achievement may have
  // been earned by the request that led here) and when the server signals one.
  if (wsMarker) {
    var TROPHY = '<svg viewBox="0 0 24 24" width="20" height="20" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M8 21h8"/><path d="M12 17v4"/><path d="M7 4h10v5a5 5 0 0 1-10 0V4Z"/><path d="M17 5h3v2a3 3 0 0 1-3 3"/><path d="M7 5H4v2a3 3 0 0 0 3 3"/></svg>';
    var toastStack = 0;
    var showAchievement = function (a) {
      var t = document.createElement('a');
      t.href = '/profile#achievements';
      t.className = 'toast toast-achievement';
      t.setAttribute('role', 'status');
      t.style.top = (84 + toastStack * 64) + 'px';
      toastStack++;
      t.innerHTML = TROPHY + '<span><span class="toast-kicker">Новое достижение</span><span class="toast-main"></span></span>';
      t.querySelector('.toast-main').textContent = a.title;
      document.body.appendChild(t);
      setTimeout(function () {
        t.classList.add('is-hiding');
        setTimeout(function () { t.remove(); toastStack = Math.max(0, toastStack - 1); }, 300);
      }, 5000);
    };
    var fetchToasts = function () {
      getJSON('/achievements/toasts').then(function (d) {
        if (d && d.achievements) d.achievements.forEach(showAchievement);
      }).catch(function () {});
    };
    fetchToasts();
    // The signal often arrives while the page is navigating away (the form
    // that earned the achievement was just submitted). Wait a moment and
    // skip it if the page is leaving, so the next page takes the toast.
    var leaving = false, toastTimer = null;
    window.addEventListener('pagehide', function () { leaving = true; clearTimeout(toastTimer); });
    document.addEventListener('forum:ws', function (e) {
      if (e.detail.type !== 'achievement') return;
      clearTimeout(toastTimer);
      toastTimer = setTimeout(function () { if (!leaving) fetchToasts(); }, 1200);
    });
  }

  // ─── Comment reactions ─────────────────────────────────────
  var closePickers = function (except) {
    document.querySelectorAll('[data-rx-picker]:not([hidden])').forEach(function (p) {
      if (p === except) return;
      p.hidden = true;
      p.parentNode.querySelector('[data-rx-toggle]').setAttribute('aria-expanded', 'false');
    });
  };
  document.addEventListener('click', function (e) {
    var toggle = e.target.closest('[data-rx-toggle]');
    if (toggle) {
      var picker = toggle.parentNode.querySelector('[data-rx-picker]');
      closePickers(picker);
      picker.hidden = !picker.hidden;
      toggle.setAttribute('aria-expanded', picker.hidden ? 'false' : 'true');
      return;
    }
    var btn = e.target.closest('button[data-rx]');
    if (!btn) {
      if (!e.target.closest('[data-rx-picker]')) closePickers(null);
      return;
    }
    var bar = btn.closest('[data-rx-bar]');
    var comments = document.querySelector('[data-comments]');
    if (!bar || !comments || bar.classList.contains('is-busy')) return;
    bar.classList.add('is-busy');
    postJSON('/comment/react', {
      comment_id: bar.getAttribute('data-rx-bar'),
      kind: btn.getAttribute('data-rx'),
      csrf_token: comments.getAttribute('data-csrf')
    }).then(function (d) {
      if (d && d.html) bar.outerHTML = d.html;
      else bar.classList.remove('is-busy');
    }).catch(function () { bar.classList.remove('is-busy'); });
  });
  document.addEventListener('keydown', function (e) { if (e.key === 'Escape') closePickers(null); });

  // ─── Polls: voting ─────────────────────────────────────────
  document.addEventListener('submit', function (e) {
    var form = e.target.closest('form[data-poll-form]');
    if (!form) return;
    e.preventDefault();
    var err = form.querySelector('[data-poll-error]');
    if (!form.querySelector('input[name=option_id]:checked')) {
      err.textContent = 'Выберите вариант ответа';
      err.hidden = false;
      return;
    }
    var submit = form.querySelector('button[type=submit]');
    submit.disabled = true;
    fetch('/poll/vote', {
      method: 'POST', body: new FormData(form), credentials: 'same-origin',
      headers: { Accept: 'application/json' }
    }).then(function (res) {
      if (res.status === 401) { window.location.href = '/login'; return null; }
      return res.json();
    }).then(function (d) {
      if (!d) return;
      if (d.html) form.closest('[data-poll]').outerHTML = d.html;
      else { err.textContent = d.error || 'Не удалось проголосовать'; err.hidden = false; submit.disabled = false; }
    }).catch(function () {
      err.textContent = 'Нет связи с сервером';
      err.hidden = false;
      submit.disabled = false;
    });
  });

  // ─── Polls: builder in the create-post form ────────────────
  var builder = document.querySelector('[data-poll-builder]');
  if (builder) {
    var enable = builder.querySelector('[data-poll-enable]');
    var fields = builder.querySelector('[data-poll-fields]');
    var optionsBox = builder.querySelector('[data-poll-options]');
    var addBtn = builder.querySelector('[data-poll-add]');
    var question = builder.querySelector('[name=poll_question]');
    var sync = function () {
      var rows = optionsBox.querySelectorAll('.poll-option-row');
      addBtn.hidden = rows.length >= 6;
      rows.forEach(function (r) { r.querySelector('[data-poll-remove]').hidden = rows.length <= 2; });
      // Only require the poll fields while the poll is switched on.
      question.required = enable.checked;
      rows.forEach(function (r, i) { r.querySelector('input').required = enable.checked && i < 2; });
    };
    enable.addEventListener('change', function () {
      fields.hidden = !enable.checked;
      sync();
      if (enable.checked) question.focus();
    });
    addBtn.addEventListener('click', function () {
      var rows = optionsBox.querySelectorAll('.poll-option-row');
      if (rows.length >= 6) return;
      var row = rows[rows.length - 1].cloneNode(true);
      row.querySelector('input').value = '';
      optionsBox.appendChild(row);
      sync();
      row.querySelector('input').focus();
    });
    optionsBox.addEventListener('click', function (e) {
      var rm = e.target.closest('[data-poll-remove]');
      if (!rm || optionsBox.querySelectorAll('.poll-option-row').length <= 2) return;
      rm.closest('.poll-option-row').remove();
      sync();
    });
    sync();
  }

  // ─── Small toasts (flash messages after moderation actions) ─
  var simpleToast = function (msg, isError) {
    var t = document.createElement('div');
    t.className = 'toast' + (isError ? ' toast-error' : '');
    t.setAttribute('role', 'status');
    var span = document.createElement('span');
    span.textContent = msg;
    t.appendChild(span);
    document.body.appendChild(t);
    setTimeout(function () { t.classList.add('is-hiding'); setTimeout(function () { t.remove(); }, 300); }, 3500);
  };
  var flash = document.cookie.split('; ').filter(function (c) { return c.indexOf('flash=') === 0; })[0];
  if (flash) {
    document.cookie = 'flash=; Max-Age=0; Path=/';
    try { simpleToast(decodeURIComponent(flash.slice(6).replace(/\+/g, ' '))); } catch (e) {}
  }
  // A ban ends the session: reload so the page reflects it.
  document.addEventListener('forum:ws', function (e) { if (e.detail.type === 'banned') location.reload(); });

  // ─── «Пожаловаться» ────────────────────────────────────────
  var reportModal = null;
  var openReport = function (btn) {
    if (!reportModal) {
      reportModal = document.createElement('div');
      reportModal.className = 'modal';
      reportModal.setAttribute('role', 'dialog');
      reportModal.setAttribute('aria-modal', 'true');
      reportModal.innerHTML = '<form class="modal-box report-box"><p class="modal-title">Пожаловаться</p>' +
        '<div class="report-reasons">' +
        [['spam', 'Спам'], ['abuse', 'Оскорбления'], ['offtopic', 'Не по теме'], ['other', 'Другое']].map(function (r, i) {
          return '<label class="poll-option"><input type="radio" name="reason" value="' + r[0] + '"' + (i ? '' : ' checked') + '><span>' + r[1] + '</span></label>';
        }).join('') + '</div>' +
        '<textarea name="details" rows="3" maxlength="500" placeholder="Пояснение (необязательно)"></textarea>' +
        '<p class="upload-error" hidden></p>' +
        '<div class="modal-actions"><button type="button" class="btn secondary" data-report-cancel>Отмена</button>' +
        '<button type="submit" class="btn">Отправить</button></div></form>';
      document.body.appendChild(reportModal);
      reportModal.querySelector('[data-report-cancel]').addEventListener('click', function () { reportModal.hidden = true; });
      reportModal.addEventListener('click', function (e) { if (e.target === reportModal) reportModal.hidden = true; });
      reportModal.querySelector('form').addEventListener('submit', function (e) {
        e.preventDefault();
        var f = e.target, err = f.querySelector('.upload-error');
        postJSON('/report', {
          target_type: reportModal.dataset.type, target_id: reportModal.dataset.id,
          reason: f.querySelector('input[name=reason]:checked').value,
          details: f.querySelector('textarea').value, csrf_token: reportModal.dataset.csrf
        }).then(function (d) {
          if (!d) return;
          if (d.error) { err.textContent = d.error; err.hidden = false; return; }
          reportModal.hidden = true;
          simpleToast(d.message);
        }).catch(function () { err.textContent = 'Нет связи с сервером'; err.hidden = false; });
      });
    }
    reportModal.dataset.type = btn.getAttribute('data-report');
    reportModal.dataset.id = btn.getAttribute('data-target');
    reportModal.dataset.csrf = btn.getAttribute('data-csrf');
    reportModal.querySelector('textarea').value = '';
    reportModal.querySelector('.upload-error').hidden = true;
    reportModal.hidden = false;
  };
  document.addEventListener('click', function (e) {
    var btn = e.target.closest('[data-report]');
    if (btn) openReport(btn);
  });

  // ─── Confirm modal for destructive forms ───────────────────
  // <form data-confirm="Вопрос?" data-confirm-text="Пояснение">
  var confirmModal = null;
  var pendingForm = null;
  var buildConfirm = function () {
    var m = document.createElement('div');
    m.className = 'modal';
    m.hidden = true;
    m.setAttribute('role', 'dialog');
    m.setAttribute('aria-modal', 'true');
    m.setAttribute('aria-labelledby', 'confirm-title');
    m.innerHTML = '<div class="modal-box"><p class="modal-title" id="confirm-title"></p><p class="modal-text"></p>' +
      '<div class="modal-actions"><button type="button" class="btn secondary" data-confirm-cancel>Отмена</button>' +
      '<button type="button" class="btn btn-danger" data-confirm-ok>Удалить</button></div></div>';
    document.body.appendChild(m);
    var close = function () { m.hidden = true; pendingForm = null; };
    m.querySelector('[data-confirm-cancel]').addEventListener('click', close);
    m.querySelector('[data-confirm-ok]').addEventListener('click', function () {
      if (pendingForm) pendingForm.submit(); // submit() skips the submit listener
    });
    m.addEventListener('click', function (e) { if (e.target === m) close(); });
    document.addEventListener('keydown', function (e) { if (e.key === 'Escape' && !m.hidden) close(); });
    return m;
  };
  document.addEventListener('submit', function (e) {
    var form = e.target.closest('form[data-confirm]');
    if (!form) return;
    e.preventDefault();
    confirmModal = confirmModal || buildConfirm();
    pendingForm = form;
    confirmModal.querySelector('.modal-title').textContent = form.getAttribute('data-confirm');
    var text = confirmModal.querySelector('.modal-text');
    text.textContent = form.getAttribute('data-confirm-text') || '';
    text.hidden = !text.textContent;
    confirmModal.hidden = false;
    confirmModal.querySelector('[data-confirm-cancel]').focus();
  });

  // ─── «⋮» menus ─────────────────────────────────────────────
  var closeMenus = function (except) {
    document.querySelectorAll('[data-menu-list]:not([hidden])').forEach(function (m) {
      if (m === except) return;
      m.hidden = true;
      var t = m.parentNode.querySelector('[data-menu-toggle]');
      if (t) t.setAttribute('aria-expanded', 'false');
    });
  };
  document.addEventListener('click', function (e) {
    var toggle = e.target.closest('[data-menu-toggle]');
    if (toggle) {
      e.preventDefault();
      var list = toggle.parentNode.querySelector('[data-menu-list]');
      closeMenus(list);
      list.hidden = !list.hidden;
      toggle.setAttribute('aria-expanded', list.hidden ? 'false' : 'true');
      return;
    }
    if (!e.target.closest('[data-menu-list]')) closeMenus(null);
    else if (e.target.closest('.menu-item')) setTimeout(function () { closeMenus(null); }, 0);
  });
  document.addEventListener('keydown', function (e) { if (e.key === 'Escape') closeMenus(null); });

  // ─── Bookmarks ─────────────────────────────────────────────
  document.addEventListener('click', function (e) {
    var btn = e.target.closest('button[data-bookmark]');
    if (!btn || btn.disabled) return;
    btn.disabled = true;
    postJSON('/bookmark', { post_id: btn.getAttribute('data-bookmark'), csrf_token: btn.getAttribute('data-csrf') })
      .then(function (d) {
        if (!d || d.error) return;
        btn.classList.toggle('is-active', d.bookmarked);
        btn.setAttribute('aria-pressed', d.bookmarked ? 'true' : 'false');
        btn.title = d.bookmarked ? 'Убрать из сохранённого' : 'Сохранить';
      })
      .catch(function () {})
      .then(function () { btn.disabled = false; });
  });

  // ─── Code blocks: highlighting and «Копировать» ────────────
  // A small tokenizer for the languages people post here; anything else is
  // shown as plain text. Output is built from escaped text only.
  var escHTML = function (s) {
    return s.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;');
  };
  var kw = function (words) { return new RegExp('\\b(?:' + words.split(' ').join('|') + ')\\b'); };
  var STR_DQ = /"(?:\\.|[^"\\\n])*"/, STR_SQ = /'(?:\\.|[^'\\\n])*'/;
  var NUM = /\b(?:0x[\da-fA-F]+|\d+(?:\.\d+)?(?:e[+-]?\d+)?)\b/;
  var LANGS = {
    go: [['comment', /\/\/[^\n]*|\/\*[\s\S]*?\*\//], ['string', /`[^`]*`/], ['string', STR_DQ], ['string', STR_SQ],
      ['keyword', kw('break case chan const continue default defer else fallthrough for func go goto if import interface map package range return select struct switch type var nil true false iota')],
      ['type', kw('string int int8 int16 int32 int64 uint uint8 uint16 uint32 uint64 float32 float64 bool byte rune error any')],
      ['number', NUM], ['func', /\b[A-Za-z_]\w*(?=\()/]],
    python: [['comment', /#[^\n]*/], ['string', /"""[\s\S]*?"""|'''[\s\S]*?'''/], ['string', STR_DQ], ['string', STR_SQ],
      ['keyword', kw('and as assert async await break class continue def del elif else except finally for from global if import in is lambda nonlocal not or pass raise return try while with yield None True False self')],
      ['number', NUM], ['func', /\b[A-Za-z_]\w*(?=\()/]],
    javascript: [['comment', /\/\/[^\n]*|\/\*[\s\S]*?\*\//], ['string', /`(?:\\.|[^`\\])*`/], ['string', STR_DQ], ['string', STR_SQ],
      ['keyword', kw('async await break case catch class const continue debugger default delete do else export extends finally for from function if import in instanceof let new of return static super switch this throw try typeof var void while yield null undefined true false')],
      ['number', NUM], ['func', /\b[A-Za-z_$][\w$]*(?=\()/]],
    sql: [['comment', /--[^\n]*|\/\*[\s\S]*?\*\//], ['string', STR_SQ], ['string', STR_DQ],
      ['keyword', /\b(?:select|from|where|and|or|not|insert|into|values|update|set|delete|create|table|index|drop|alter|add|join|left|right|inner|outer|on|group|by|order|asc|desc|limit|offset|having|as|distinct|null|is|in|like|exists|primary|key|foreign|references|default|unique|count|sum|avg|min|max|case|when|then|else|end|union|all)\b/],
      ['number', NUM]],
    html: [['comment', /<!--[\s\S]*?-->/], ['tag', /<\/?[A-Za-z][\w-]*|\/?>/], ['attr', /\b[\w-]+(?==)/], ['string', STR_DQ], ['string', STR_SQ]],
    css: [['comment', /\/\*[\s\S]*?\*\//], ['string', STR_DQ], ['string', STR_SQ], ['keyword', /@[\w-]+/],
      ['attr', /[\w-]+(?=\s*:)/], ['number', /#[\da-fA-F]{3,8}\b|-?\b\d+(?:\.\d+)?(?:px|em|rem|%|vh|vw|s|ms|deg|fr)?\b/],
      ['tag', /[.#]?[A-Za-z_][\w-]*(?=[^{};]*\{)/]]
  };
  var ALIAS = { go: 'go', golang: 'go', py: 'python', python: 'python', js: 'javascript', javascript: 'javascript',
    ts: 'javascript', typescript: 'javascript', jsx: 'javascript', sql: 'sql', sqlite: 'sql', html: 'html',
    xml: 'html', css: 'css' };

  var highlight = function (text, lang) {
    var name = ALIAS[lang];
    var rules = LANGS[name];
    if (!rules) return escHTML(text);
    var flags = 'g' + (name === 'sql' ? 'i' : '');
    var re = new RegExp(rules.map(function (r) { return '(' + r[1].source + ')'; }).join('|'), flags);
    var out = '', last = 0, m;
    while ((m = re.exec(text)) !== null) {
      if (m[0] === '') { re.lastIndex++; continue; }
      var type = 'plain';
      for (var i = 1; i < m.length; i++) if (m[i] !== undefined) { type = rules[i - 1][0]; break; }
      out += escHTML(text.slice(last, m.index)) + '<span class="tok-' + type + '">' + escHTML(m[0]) + '</span>';
      last = re.lastIndex;
    }
    return out + escHTML(text.slice(last));
  };

  var enhanceCode = function (root) {
    root.querySelectorAll('.code-block code[data-lang]:not([data-hl])').forEach(function (code) {
      code.setAttribute('data-hl', '');
      code.innerHTML = highlight(code.textContent, code.getAttribute('data-lang'));
    });
  };
  enhanceCode(document);

  document.addEventListener('click', function (e) {
    var btn = e.target.closest('[data-code-copy]');
    if (!btn) return;
    var text = btn.closest('.code-block').querySelector('code').textContent;
    var done = function (ok) {
      btn.textContent = ok ? 'Скопировано' : 'Не удалось';
      setTimeout(function () { btn.textContent = 'Копировать'; }, 1500);
    };
    if (navigator.clipboard && window.isSecureContext) {
      navigator.clipboard.writeText(text).then(function () { done(true); }, function () { done(false); });
    } else {
      // Plain http on a LAN address has no Clipboard API: fall back to execCommand.
      var ta = document.createElement('textarea');
      ta.value = text;
      ta.style.position = 'fixed';
      ta.style.opacity = '0';
      document.body.appendChild(ta);
      ta.select();
      var ok = false;
      try { ok = document.execCommand('copy'); } catch (err) { ok = false; }
      ta.remove();
      done(ok);
    }
  });

  // ─── Comment threads: reply, collapse, edit in place ───────
  var replyTpl = document.getElementById('reply-form-tpl');
  document.addEventListener('click', function (e) {
    var replyBtn = e.target.closest('button[data-reply]');
    if (replyBtn && replyTpl) {
      var comment = replyBtn.closest('[data-comment]');
      var slot = comment.querySelector(':scope > [data-reply-slot]');
      if (slot.firstElementChild) { slot.querySelector('textarea').focus(); return; }
      var form = replyTpl.content.firstElementChild.cloneNode(true);
      form.querySelector('[name=parent_id]').value = comment.getAttribute('data-comment');
      var ta = form.querySelector('textarea');
      // Past the last nesting level the reply joins the thread above, so
      // name the person being answered.
      if (Number(comment.getAttribute('data-depth')) >= 3) ta.value = '@' + comment.getAttribute('data-author') + ' ';
      form.querySelector('[data-reply-cancel]').addEventListener('click', function () { form.remove(); });
      slot.appendChild(form);
      ta.focus();
      ta.setSelectionRange(ta.value.length, ta.value.length);
      return;
    }

    var toggle = e.target.closest('[data-thread-toggle]');
    if (toggle) {
      var replies = toggle.nextElementSibling;
      var open = toggle.getAttribute('aria-expanded') === 'true';
      replies.hidden = open;
      toggle.setAttribute('aria-expanded', open ? 'false' : 'true');
      toggle.textContent = toggle.getAttribute(open ? 'data-show' : 'data-hide');
      return;
    }

    var editBtn = e.target.closest('[data-comment-edit]');
    if (editBtn) {
      var c = editBtn.closest('[data-comment]');
      var body = c.querySelector(':scope > [data-comment-body]');
      if (c.querySelector(':scope > .comment-edit-form')) return;
      var source = c.querySelector(':scope > [data-comment-source]');
      var f = document.createElement('form');
      f.className = 'comment-form comment-edit-form';
      f.innerHTML = '<textarea rows="4" maxlength="10000" required data-mentions></textarea>' +
        '<p class="upload-error" hidden></p>' +
        '<div class="comment-form-actions"><button type="button" class="btn secondary small" data-edit-cancel>Отмена</button>' +
        '<button type="submit" class="btn small">Сохранить</button></div>';
      var area = f.querySelector('textarea');
      area.value = source.value;
      body.hidden = true;
      body.after(f);
      area.focus();
      var done = function () { f.remove(); body.hidden = false; };
      f.querySelector('[data-edit-cancel]').addEventListener('click', done);
      f.addEventListener('submit', function (ev) {
        ev.preventDefault();
        var csrf = document.querySelector('[data-comments]').getAttribute('data-csrf');
        var err = f.querySelector('.upload-error');
        postJSON('/comment/edit', { comment_id: c.getAttribute('data-comment'), content: area.value, csrf_token: csrf })
          .then(function (d) {
            if (!d) return;
            if (d.error) { err.textContent = d.error; err.hidden = false; return; }
            body.innerHTML = d.html;
            source.value = d.content;
            enhanceCode(body);
            if (!c.querySelector(':scope > .comment-head [data-edited]')) {
              var badge = document.createElement('span');
              badge.className = 'edited-badge';
              badge.setAttribute('data-edited', '');
              badge.textContent = 'изменено';
              badge.title = 'Изменено только что';
              c.querySelector(':scope > .comment-head .comment-meta').appendChild(badge);
            }
            done();
          })
          .catch(function () { err.textContent = 'Нет связи с сервером'; err.hidden = false; });
      });
    }
  });

  // ─── Markdown editor: toolbar and preview ──────────────────
  // Replace ta.value[from:to] with text through the browser's own editing
  // command, so the change lands in the undo history (⌘Z / Ctrl+Z).
  // setRangeText or assigning .value would silently clear that history.
  var replaceText = function (ta, from, to, text) {
    ta.focus();
    ta.setSelectionRange(from, to);
    var ok = false;
    try { ok = document.execCommand('insertText', false, text); } catch (e) { ok = false; }
    if (!ok) { // very old browsers: still works, just without undo
      ta.setRangeText(text, from, to, 'end');
      ta.dispatchEvent(new Event('input', { bubbles: true }));
    }
  };

  var applyFormat = function (ta, kind) {
    var v = ta.value;
    var start = ta.selectionStart, end = ta.selectionEnd;
    var sel = v.slice(start, end);

    var wrap = function (left, right, placeholder) {
      var text = sel || placeholder;
      replaceText(ta, start, end, left + text + right);
      ta.setSelectionRange(start + left.length, start + left.length + text.length);
    };
    // Is the selection directly surrounded by the marker? For «*» make sure
    // it isn't half of a bold «**» (but «***» is bold + italic).
    var around = function (mk) {
      if (v.slice(start - mk.length, start) !== mk || v.slice(end, end + mk.length) !== mk) return false;
      if (mk === '*') {
        var b2 = v.slice(start - 2, start) === '**', a2 = v.slice(end, end + 2) === '**';
        var b3 = v.slice(start - 3, start) === '***', a3 = v.slice(end, end + 3) === '***';
        if ((b2 && !b3) || (a2 && !a3)) return false;
      }
      return true;
    };
    // Pressing the same button again removes the formatting.
    var toggle = function (mk, placeholder) {
      var m = mk.length;
      if (sel.length > 2 * m && sel.slice(0, m) === mk && sel.slice(-m) === mk) {
        var inner = sel.slice(m, -m);
        replaceText(ta, start, end, inner);
        ta.setSelectionRange(start, start + inner.length);
      } else if (sel && around(mk)) {
        replaceText(ta, start - m, end + m, sel);
        ta.setSelectionRange(start - m, start - m + sel.length);
      } else {
        wrap(mk, mk, placeholder);
      }
    };
    var prefixLines = function (prefix) {
      var from = v.lastIndexOf('\n', start - 1) + 1;
      var lines = (v.slice(from, end) || 'текст').split('\n');
      // Toggle too: if every line already has the prefix, remove it.
      var all = lines.every(function (l) { return l.slice(0, prefix.length) === prefix; });
      var text = lines.map(function (l) { return all ? l.slice(prefix.length) : prefix + l; }).join('\n');
      replaceText(ta, from, end, text);
      ta.setSelectionRange(from, from + text.length);
    };

    switch (kind) {
      case 'bold': toggle('**', 'жирный текст'); break;
      case 'italic': toggle('*', 'курсив'); break;
      case 'code': toggle('`', 'код'); break;
      case 'codeblock': {
        var nl = start > 0 && v[start - 1] !== '\n' ? '\n' : '';
        wrap(nl + '```go\n', '\n```\n', 'код');
        break;
      }
      case 'link': {
        var label = sel || 'текст ссылки';
        replaceText(ta, start, end, '[' + label + '](https://)');
        var urlStart = start + label.length + 3;
        ta.setSelectionRange(urlStart, urlStart + 8);
        break;
      }
      case 'list': prefixLines('- '); break;
      case 'quote': prefixLines('> '); break;
    }
  };

  document.querySelectorAll('[data-md-editor]').forEach(function (ed) {
    var ta = ed.querySelector('[data-md-input]');
    var preview = ed.querySelector('[data-md-preview]');
    var tools = ed.querySelector('[data-md-tools]');
    ed.querySelectorAll('[data-md]').forEach(function (b) {
      b.addEventListener('click', function () { applyFormat(ta, b.getAttribute('data-md')); });
    });
    ta.addEventListener('keydown', function (e) {
      if (!(e.ctrlKey || e.metaKey)) return;
      if (e.key === 'b') { e.preventDefault(); applyFormat(ta, 'bold'); }
      if (e.key === 'i') { e.preventDefault(); applyFormat(ta, 'italic'); }
    });
    ed.querySelectorAll('[data-md-tab]').forEach(function (tab) {
      tab.addEventListener('click', function () {
        var showPreview = tab.getAttribute('data-md-tab') === 'preview';
        ed.querySelectorAll('[data-md-tab]').forEach(function (t) {
          t.classList.toggle('is-active', t === tab);
          t.setAttribute('aria-selected', t === tab ? 'true' : 'false');
        });
        ta.hidden = showPreview;
        tools.classList.toggle('is-disabled', showPreview);
        preview.hidden = !showPreview;
        if (!showPreview) { ta.focus(); return; }
        preview.innerHTML = '<p class="md-empty">Загрузка…</p>';
        postJSON('/markdown/preview', { text: ta.value, headings: '1', csrf_token: ed.getAttribute('data-csrf') })
          .then(function (d) {
            preview.innerHTML = d && d.html ? d.html : '<p class="md-empty">Не удалось показать предпросмотр</p>';
            enhanceCode(preview);
          })
          .catch(function () { preview.innerHTML = '<p class="md-empty">Нет связи с сервером</p>'; });
      });
    });
    // A hidden required field can't show its error: switch back to «Текст».
    ta.addEventListener('invalid', function () {
      if (ta.hidden) ed.querySelector('[data-md-tab="write"]').click();
    });
  });

  // ─── @mentions autocomplete ────────────────────────────────
  var mention = { box: null, ta: null, items: [], active: 0, start: 0, timer: null };
  var closeMention = function () {
    if (mention.box) mention.box.hidden = true;
    mention.items = [];
  };
  var pickMention = function (i) {
    var u = mention.items[i];
    if (!u) return;
    var ta = mention.ta;
    replaceText(ta, mention.start, ta.selectionStart, '@' + u.username + ' ');
    closeMention();
  };
  var renderMention = function () {
    var box = mention.box;
    box.innerHTML = '';
    mention.items.forEach(function (u, i) {
      var row = document.createElement('button');
      row.type = 'button';
      row.className = 'mention-item' + (i === mention.active ? ' is-active' : '');
      row.setAttribute('role', 'option');
      var av;
      if (u.avatar) {
        av = document.createElement('img');
        av.className = 'avatar avatar-32';
        av.src = u.avatar;
        av.alt = '';
      } else {
        av = document.createElement('span');
        av.className = 'avatar avatar-32 avatar-fallback';
        av.style.background = u.color;
        av.textContent = u.initial;
      }
      var name = document.createElement('span');
      name.textContent = '@' + u.username;
      row.appendChild(av);
      row.appendChild(name);
      row.addEventListener('mousedown', function (e) { e.preventDefault(); pickMention(i); });
      box.appendChild(row);
    });
    box.hidden = mention.items.length === 0;
  };
  document.addEventListener('input', function (e) {
    var ta = e.target;
    if (!ta.matches || !ta.matches('textarea[data-mentions]')) return;
    var upto = ta.value.slice(0, ta.selectionStart);
    var m = upto.match(/(^|[^\w@])@([A-Za-z0-9_]{1,20})$/);
    clearTimeout(mention.timer);
    if (!m) { closeMention(); return; }
    mention.ta = ta;
    mention.start = ta.selectionStart - m[2].length - 1;
    mention.timer = setTimeout(function () {
      getJSON('/users/search?q=' + encodeURIComponent(m[2])).then(function (d) {
        if (mention.ta !== ta) return;
        if (!mention.box) {
          mention.box = document.createElement('div');
          mention.box.className = 'mention-box';
          mention.box.setAttribute('role', 'listbox');
        }
        // Keep the list right under the field being typed in.
        ta.parentNode.classList.add('mention-host');
        ta.after(mention.box);
        mention.items = (d && d.users) || [];
        mention.active = 0;
        renderMention();
      }).catch(function () {});
    }, 150);
  });
  document.addEventListener('keydown', function (e) {
    if (!mention.box || mention.box.hidden || e.target !== mention.ta) return;
    if (e.key === 'ArrowDown' || e.key === 'ArrowUp') {
      e.preventDefault();
      var n = mention.items.length;
      mention.active = (mention.active + (e.key === 'ArrowDown' ? 1 : n - 1)) % n;
      renderMention();
    } else if (e.key === 'Enter' || e.key === 'Tab') {
      e.preventDefault();
      pickMention(mention.active);
    } else if (e.key === 'Escape') {
      closeMention();
    }
  });
  document.addEventListener('focusout', function (e) {
    if (e.target === mention.ta) setTimeout(closeMention, 150);
  });

  // ─── Lightbox for post images ──────────────────────────────
  document.addEventListener('click', function (e) {
    var trigger = e.target.closest('[data-lightbox]');
    if (!trigger) return;
    var box = document.createElement('div');
    box.className = 'lightbox';
    box.setAttribute('role', 'dialog');
    box.setAttribute('aria-modal', 'true');
    box.setAttribute('aria-label', 'Картинка');
    box.innerHTML = '<button type="button" class="viewer-close" aria-label="Закрыть">&times;</button><img alt="">';
    box.querySelector('img').src = trigger.getAttribute('data-lightbox');
    var close = function () {
      box.remove();
      document.body.style.overflow = '';
      document.removeEventListener('keydown', onKey);
      trigger.focus();
    };
    var onKey = function (ev) { if (ev.key === 'Escape') close(); };
    box.addEventListener('click', function (ev) { if (ev.target.tagName !== 'IMG') close(); });
    document.addEventListener('keydown', onKey);
    document.body.appendChild(box);
    document.body.style.overflow = 'hidden';
    box.querySelector('.viewer-close').focus();
  });

  // ─── Image field on the create-post form ───────────────────
  var field = document.querySelector('[data-image-field]');
  if (field) {
    var fileInput = field.querySelector('[data-image-input]');
    var pick = field.querySelector('[data-image-pick]');
    var preview = field.querySelector('[data-image-preview]');
    var err = field.querySelector('[data-image-error]');
    var TYPES = ['image/jpeg', 'image/png', 'image/gif', 'image/webp'];

    var removed = field.querySelector('[data-image-removed]');
    var reset = function (msg) {
      fileInput.value = '';
      preview.hidden = true;
      pick.hidden = false;
      // On the edit page this also drops the post's current image.
      if (removed) removed.value = '1';
      err.textContent = msg || '';
      err.hidden = !msg;
    };
    fileInput.addEventListener('change', function () {
      var file = fileInput.files && fileInput.files[0];
      if (!file) { reset(); return; }
      if (TYPES.indexOf(file.type) === -1) { reset('Можно загрузить только JPG, PNG, GIF или WEBP'); return; }
      if (file.size > 10 * 1024 * 1024) { reset('Файл слишком большой (максимум 10 МБ)'); return; }
      var reader = new FileReader();
      reader.onload = function (ev) {
        preview.querySelector('img').src = ev.target.result;
        preview.hidden = false;
        pick.hidden = true;
        err.hidden = true;
      };
      reader.readAsDataURL(file);
    });
    field.querySelector('[data-image-remove]').addEventListener('click', function () { reset(); });
  }
})();

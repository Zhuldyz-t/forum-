(function () {
  'use strict';

  // ─── Toasts ────────────────────────────────────────────────
  var CHECK_ICON = '<svg viewBox="0 0 24 24" width="18" height="18" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M20 6 9 17l-5-5"/></svg>';

  var hideToast = function (toast) {
    if (!toast.isConnected) return;
    toast.classList.add('is-hiding');
    setTimeout(function () { toast.remove(); }, 300);
  };
  var armToast = function (toast) {
    toast.querySelector('.toast-close').addEventListener('click', function () { hideToast(toast); });
    setTimeout(function () { hideToast(toast); }, 3000);
  };
  var ERROR_ICON = '<svg viewBox="0 0 24 24" width="18" height="18" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><circle cx="12" cy="12" r="9"/><path d="M12 8v4"/><path d="M12 16h.01"/></svg>';

  var showToast = function (msg, isError) {
    var toast = document.createElement('div');
    toast.className = 'toast' + (isError ? ' toast-error' : '');
    toast.setAttribute('role', 'status');
    toast.innerHTML = (isError ? ERROR_ICON : CHECK_ICON) + '<span></span><button type="button" class="toast-close" aria-label="Закрыть">&times;</button>';
    toast.querySelector('span').textContent = msg;
    document.body.appendChild(toast);
    armToast(toast);
  };

  var serverToast = document.querySelector('.toast[data-autohide]');
  if (serverToast) {
    document.body.appendChild(serverToast); // keep it out of the page flow
    armToast(serverToast);
    // Drop the one-shot flag so a reload doesn't show the message again.
    var url = new URL(window.location.href);
    if (url.searchParams.has('updated') || url.searchParams.has('msg')) {
      url.searchParams.delete('updated');
      url.searchParams.delete('msg');
      window.history.replaceState(null, '', url.pathname + url.search + url.hash);
    }
  }

  var MAX_SIZE = 10 * 1024 * 1024;

  // ─── Shared fetch helper for the JSON endpoints ─────────────
  var postForm = function (url, form) {
    return fetch(url, { method: 'POST', body: form, credentials: 'same-origin' })
      .then(function (res) {
        return res.json().catch(function () { return { error: 'Ошибка сервера, попробуйте позже' }; });
      }, function () {
        return { error: 'Нет связи с сервером, попробуйте ещё раз' };
      });
  };

  // ─── Profile cover: upload, gradients, reset ───────────────
  document.querySelectorAll('[data-cover]').forEach(function (cover) {
    var csrf = cover.getAttribute('data-csrf');
    var toggle = cover.querySelector('[data-cover-toggle]');
    if (!toggle) return; // cover controls exist only on the edit page
    var menu = cover.querySelector('[data-cover-menu]');
    var input = cover.querySelector('[data-cover-input]');
    var COVER_TYPES = ['image/jpeg', 'image/png', 'image/webp'];

    var setMenu = function (open) {
      menu.hidden = !open;
      toggle.setAttribute('aria-expanded', open ? 'true' : 'false');
    };
    toggle.addEventListener('click', function (e) {
      e.stopPropagation();
      setMenu(menu.hidden);
    });
    document.addEventListener('click', function (e) {
      if (!menu.hidden && !menu.contains(e.target)) setMenu(false);
    });
    document.addEventListener('keydown', function (e) {
      if (e.key === 'Escape' && !menu.hidden) { setMenu(false); toggle.focus(); }
    });

    // Reflect the server's answer: uploaded image or a gradient class.
    var apply = function (state) {
      Array.from(cover.classList).forEach(function (c) {
        if (c.indexOf('cover-') === 0) cover.classList.remove(c); // old gradient
      });
      cover.classList.add('cover-' + state.preset);
      cover.style.backgroundImage = state.image ? "url('" + state.image + "')" : '';
      cover.querySelectorAll('.cover-swatch').forEach(function (sw) {
        sw.classList.toggle('is-active', !state.image && sw.getAttribute('data-preset') === state.preset);
      });
    };
    var send = function (url, form, okMsg) {
      form.append('csrf_token', csrf);
      setMenu(false);
      cover.classList.add('is-loading');
      postForm(url, form).then(function (data) {
        cover.classList.remove('is-loading');
        if (data.error) { showToast(data.error, true); return; }
        apply(data);
        showToast(okMsg);
      });
    };

    cover.querySelector('[data-cover-upload]').addEventListener('click', function () { input.click(); });
    input.addEventListener('change', function () {
      var file = input.files && input.files[0];
      input.value = '';
      if (!file) return;
      if (COVER_TYPES.indexOf(file.type) === -1) { showToast('Можно загрузить только JPG, PNG или WEBP', true); return; }
      if (file.size > MAX_SIZE) { showToast('Файл слишком большой (максимум 10 МБ)', true); return; }
      var form = new FormData();
      form.append('cover', file);
      send('/profile/cover/upload', form, 'Обложка обновлена');
    });
    cover.querySelectorAll('.cover-swatch').forEach(function (sw) {
      sw.addEventListener('click', function () {
        var form = new FormData();
        form.append('preset', sw.getAttribute('data-preset'));
        send('/profile/cover/preset', form, 'Обложка обновлена');
      });
    });
    cover.querySelector('[data-cover-delete]').addEventListener('click', function () {
      send('/profile/cover/delete', new FormData(), 'Обложка сброшена');
    });
  });

  // ─── Instant avatar upload ─────────────────────────────────
  var ALLOWED = ['image/jpeg', 'image/png', 'image/gif', 'image/webp'];

  var uploader = document.getElementById('avatar-uploader');
  if (uploader) {
    var input = document.getElementById('avatar-input');
    var trigger = document.getElementById('avatar-upload');
    var preview = document.getElementById('avatar-preview');
    var errorEl = document.getElementById('upload-error');
    var gallery = document.getElementById('photo-gallery');

    var showError = function (msg) {
      errorEl.textContent = msg;
      errorEl.hidden = !msg;
    };
    var setAvatar = function (src) {
      var img = document.createElement('img');
      img.src = src;
      img.alt = 'Текущее фото';
      preview.innerHTML = '';
      preview.appendChild(img);
      // Keep the header avatar in sync.
      var head = document.querySelector('.user-greeting .avatar');
      if (head) {
        var headImg = document.createElement('img');
        headImg.className = 'avatar avatar-32';
        headImg.src = src;
        headImg.alt = '';
        head.replaceWith(headImg);
      }
    };

    trigger.addEventListener('click', function () {
      if (!uploader.classList.contains('is-loading')) input.click();
    });

    input.addEventListener('change', function () {
      var file = input.files && input.files[0];
      input.value = ''; // allow picking the same file again
      showError('');
      if (!file) return;

      if (ALLOWED.indexOf(file.type) === -1) {
        showError('Можно загрузить только JPG, PNG, GIF или WEBP');
        return;
      }
      if (file.size > MAX_SIZE) {
        showError('Файл слишком большой (максимум 10 МБ)');
        return;
      }

      var form = new FormData();
      form.append('csrf_token', uploader.getAttribute('data-csrf'));
      form.append('avatar', file);

      uploader.classList.add('is-loading');
      trigger.setAttribute('aria-busy', 'true');
      postForm('/profile/photo/upload', form)
        .then(function (data) {
          if (data.error) {
            showError(data.error);
            return;
          }
          setAvatar(data.avatar);
          if (gallery && data.gallery) gallery.innerHTML = data.gallery;
          showToast('Фото обновлено');
        })
        .then(function () {
          uploader.classList.remove('is-loading');
          trigger.removeAttribute('aria-busy');
        });
    });
  }

  // ─── Bio character counter ─────────────────────────────────
  var bio = document.getElementById('bio-input');
  if (bio) {
    var counter = document.getElementById('bio-counter');
    var max = parseInt(bio.getAttribute('maxlength'), 10);
    var update = function () {
      var n = Array.from(bio.value).length;
      counter.textContent = n + ' / ' + max;
      counter.classList.toggle('is-near-limit', n >= max - 20);
    };
    bio.addEventListener('input', update);
    update();
  }

  // Delete confirmations (form[data-confirm]) are handled by site.js.

  // ─── Profile tabs ──────────────────────────────────────────
  var tabs = document.querySelectorAll('.tab[data-tab]');
  tabs.forEach(function (tab) {
    tab.addEventListener('click', function (e) {
      e.preventDefault();
      var name = tab.getAttribute('data-tab');
      tabs.forEach(function (t) {
        var active = t === tab;
        t.classList.toggle('is-active', active);
        t.setAttribute('aria-selected', active ? 'true' : 'false');
      });
      document.querySelectorAll('.tab-panel[data-panel]').forEach(function (p) {
        p.hidden = p.getAttribute('data-panel') !== name;
      });
      var url = new URL(window.location.href);
      url.searchParams.set('tab', name);
      window.history.replaceState(null, '', url.pathname + url.search + url.hash);
    });
  });

  // ─── Photo viewer ──────────────────────────────────────────
  var viewer = document.getElementById('photo-viewer');
  var opener = document.getElementById('open-viewer');
  if (!viewer || !opener) return;

  var srcs = Array.prototype.map.call(viewer.querySelectorAll('li[data-src]'), function (li) {
    return li.getAttribute('data-src');
  });
  var img = viewer.querySelector('.viewer-img');
  var count = viewer.querySelector('.viewer-counter');
  var prev = viewer.querySelector('[data-viewer-prev]');
  var next = viewer.querySelector('[data-viewer-next]');
  var index = 0;

  if (srcs.length < 2) {
    prev.hidden = true;
    next.hidden = true;
  }

  var show = function (i) {
    index = (i + srcs.length) % srcs.length;
    img.src = srcs[index];
    count.textContent = srcs.length > 1 ? (index + 1) + ' / ' + srcs.length : '';
  };
  var open = function () {
    show(0);
    viewer.hidden = false;
    document.body.style.overflow = 'hidden';
  };
  var close = function () {
    viewer.hidden = true;
    document.body.style.overflow = '';
    opener.focus();
  };

  opener.addEventListener('click', open);
  prev.addEventListener('click', function () { show(index - 1); });
  next.addEventListener('click', function () { show(index + 1); });
  viewer.querySelector('[data-viewer-close]').addEventListener('click', close);
  // Click on the dark backdrop (not on the photo or controls) closes it.
  viewer.addEventListener('click', function (e) {
    if (e.target === viewer) close();
  });
  document.addEventListener('keydown', function (e) {
    if (viewer.hidden) return;
    if (e.key === 'Escape') close();
    else if (e.key === 'ArrowLeft' && srcs.length > 1) show(index - 1);
    else if (e.key === 'ArrowRight' && srcs.length > 1) show(index + 1);
  });

  // Swipe on touch screens.
  var touchX = null;
  viewer.addEventListener('touchstart', function (e) { touchX = e.touches[0].clientX; }, { passive: true });
  viewer.addEventListener('touchend', function (e) {
    if (touchX === null || srcs.length < 2) return;
    var dx = e.changedTouches[0].clientX - touchX;
    if (Math.abs(dx) > 40) show(index + (dx < 0 ? 1 : -1));
    touchX = null;
  });
})();

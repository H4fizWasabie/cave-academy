// Chat enhancement for /ai-nlp-bots/{slug}. Without this file the form still
// works as a plain POST + redirect. Message text only ever goes in through
// textContent / createTextNode, never innerHTML.
(function () {
  var form = document.querySelector('[data-chat-form]');
  if (!form) return;
  var log = document.querySelector('[data-log]');
  var input = form.elements.message;
  var send = form.querySelector('.chat__send');
  var chips = document.querySelector('[data-chips]');
  var count = document.querySelector('[data-count]');
  var busy = false;

  // Same rules as formatBody in chat.go: **bold** and bare http(s) links.
  var inline = /\*\*[^*\n]+\*\*|https?:\/\/[^\s<>"'*]+/g;
  function fill(el, text) {
    var last = 0, m;
    inline.lastIndex = 0;
    while ((m = inline.exec(text))) {
      el.appendChild(document.createTextNode(text.slice(last, m.index)));
      var tok = m[0];
      last = m.index + tok.length;
      if (tok.slice(0, 2) === '**') {
        var b = document.createElement('strong');
        b.textContent = tok.slice(2, -2);
        el.appendChild(b);
      } else {
        var link = tok.replace(/[.,;:!?)\]]+$/, '');
        last = m.index + link.length;
        var a = document.createElement('a');
        a.href = link;
        a.target = '_blank';
        a.rel = 'noopener noreferrer nofollow';
        a.textContent = link;
        el.appendChild(a);
      }
    }
    el.appendChild(document.createTextNode(text.slice(last)));
  }

  function bubble(kind, text) {
    var row = document.createElement('div');
    row.className = 'msg msg--' + kind;
    var body = document.createElement('div');
    body.className = 'msg__body';
    if (text === null) {
      body.className += ' msg__typing';
      var hint = document.createElement('span');
      hint.className = 'sr-only';
      hint.textContent = 'Bot sedang menaip...';
      body.appendChild(hint);
      for (var i = 0; i < 3; i++) body.appendChild(document.createElement('i'));
    } else {
      fill(body, text);
    }
    row.appendChild(body);
    log.appendChild(row);
    log.scrollTop = log.scrollHeight;
    return row;
  }

  function setCount(n) {
    if (!count || typeof n !== 'number' || n < 0) return;
    count.textContent = 'Baki soalan hari ini: ' + n + '/' + count.getAttribute('data-limit');
  }

  function grow() {
    input.style.height = 'auto';
    input.style.height = Math.min(input.scrollHeight, 140) + 'px';
  }

  if (chips) {
    chips.hidden = false;
    chips.addEventListener('click', function (e) {
      var b = e.target.closest('[data-example]');
      if (!b) return;
      input.value = b.textContent;
      grow();
      input.focus();
    });
  }

  input.addEventListener('input', grow);
  input.addEventListener('keydown', function (e) {
    // Enter sends with a physical keyboard; on phones it stays a new line.
    if (e.key === 'Enter' && !e.shiftKey && !e.isComposing && window.matchMedia('(pointer: fine)').matches) {
      e.preventDefault();
      form.requestSubmit();
    }
  });

  form.addEventListener('submit', function (e) {
    e.preventDefault();
    var text = input.value.trim();
    if (busy || !text) return;
    busy = true;
    send.disabled = true;
    if (chips) chips.hidden = true;
    var errBox = form.parentNode.querySelector('.chat__error');
    if (errBox) errBox.remove();
    Array.prototype.forEach.call(log.querySelectorAll('.msg--error'), function (el) { el.remove(); });
    Array.prototype.forEach.call(log.querySelectorAll('.msg--failed'), function (el) { el.remove(); });
    var mine = bubble('user', text);
    input.value = '';
    grow();
    var typing = bubble('assistant', null);

    function fail(msg) {
      typing.remove();
      mine.className += ' msg--failed'; // the retry sends it again: drop this copy then
      var row = bubble('error', msg);
      row.setAttribute('role', 'alert');
      var retry = document.createElement('button');
      retry.type = 'button';
      retry.className = 'msg__retry';
      retry.textContent = 'Cuba lagi';
      retry.addEventListener('click', function () { form.requestSubmit(); });
      row.firstChild.appendChild(document.createElement('br'));
      row.firstChild.appendChild(retry);
      log.scrollTop = log.scrollHeight;
      input.value = text; // the question was not used up: let them retry
      grow();
    }

    fetch(form.getAttribute('action'), {
      method: 'POST',
      credentials: 'same-origin',
      headers: { 'Accept': 'application/json', 'Content-Type': 'application/x-www-form-urlencoded' },
      body: new URLSearchParams({ message: text }).toString()
    }).then(function (res) {
      return res.json().catch(function () { return {}; }).then(function (data) {
        return { status: res.status, ok: res.ok, data: data };
      });
    }).then(function (r) {
      if (r.status === 401 && r.data.login) { window.location.href = r.data.login; return; }
      setCount(r.data.remaining);
      if (r.ok && r.data.reply) {
        typing.remove();
        bubble('assistant', r.data.reply);
      } else {
        fail(r.data.error || 'Maaf, berlaku ralat. Sila cuba lagi.');
      }
    }).catch(function () {
      fail('Maaf, tidak dapat menghubungi pelayan. Semak sambungan internet anda dan cuba lagi.');
    }).then(function () {
      busy = false;
      send.disabled = false;
      input.focus();
    });
  });

  log.scrollTop = log.scrollHeight;
})();

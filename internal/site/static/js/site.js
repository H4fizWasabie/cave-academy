// Mobile menu: the hamburger toggles the nav open/closed. While open, Escape
// closes it, Tab stays inside the header and the page behind does not scroll.
(function () {
  var header = document.getElementById('site-header');
  var btn = document.querySelector('[data-nav-toggle]');
  if (!header || !btn) return;
  var focusable = 'a[href], button:not([disabled])';

  function setOpen(open, restoreFocus) {
    header.classList.toggle('is-open', open);
    document.body.classList.toggle('nav-locked', open);
    btn.setAttribute('aria-expanded', open);
    btn.setAttribute('aria-label', open ? 'Tutup menu' : 'Buka menu');
    if (!open && restoreFocus) btn.focus();
  }

  btn.addEventListener('click', function () {
    setOpen(!header.classList.contains('is-open'));
  });

  document.addEventListener('keydown', function (e) {
    if (!header.classList.contains('is-open')) return;
    if (e.key === 'Escape') {
      setOpen(false, true);
    } else if (e.key === 'Tab') {
      var items = Array.prototype.filter.call(header.querySelectorAll(focusable), function (el) {
        return el.offsetParent !== null; // skip anything hidden by CSS
      });
      var first = items[0], last = items[items.length - 1];
      if (e.shiftKey && document.activeElement === first) { e.preventDefault(); last.focus(); }
      else if (!e.shiftKey && document.activeElement === last) { e.preventDefault(); first.focus(); }
    }
  });

  // Resizing up to the desktop layout leaves the menu always visible: drop the lock.
  window.matchMedia('(min-width: 1280px)').addEventListener('change', function (e) {
    if (e.matches) setOpen(false);
  });
})();

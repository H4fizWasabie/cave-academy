// Mobile menu: the hamburger toggles the nav open/closed.
(function () {
  var header = document.getElementById('site-header');
  var btn = document.querySelector('[data-nav-toggle]');
  if (!header || !btn) return;
  btn.addEventListener('click', function () {
    var open = header.classList.toggle('is-open');
    btn.setAttribute('aria-expanded', open);
    btn.setAttribute('aria-label', open ? 'Tutup menu' : 'Buka menu');
  });
})();

// Live preview on /trainer/profil: mirrors the form into the profile card
// (same markup as /trainer/{slug}) and switches the template class.
(function () {
  var form = document.querySelector('[data-tp-form]');
  var card = document.getElementById('tp-preview');
  if (!form || !card) return;

  function f(name) { return card.querySelector('[data-f="' + name + '"]'); }
  function section(name) { return card.querySelector('[data-s="' + name + '"]'); }
  function val(name) { return form.elements[name].value.trim().replace(/\s+/g, ' '); }
  function show(el, on) { if (el) el.hidden = !on; }

  function update() {
    var name = val('display_name');
    var initials = name.split(' ').filter(Boolean).slice(0, 2).map(function (w) { return Array.from(w)[0]; }).join('').toUpperCase();
    f('initials').textContent = initials;
    f('name').textContent = name || 'Nama anda';

    var headline = val('headline');
    f('headline').textContent = headline;
    show(f('headline'), headline);

    var bio = form.elements.bio.value.trim();
    f('bio').textContent = bio;
    show(section('bio'), bio);

    var tags = f('tags');
    tags.textContent = '';
    val('expertise').split(',').forEach(function (t) {
      t = t.trim();
      if (!t) return;
      var li = document.createElement('li');
      li.textContent = t;
      tags.appendChild(li);
    });

    f('years').textContent = (parseInt(val('years'), 10) || 0) + ' tahun';

    var location = val('location');
    f('location').textContent = location;
    show(section('location'), location);

    var phone = val('phone');
    f('phone').textContent = phone;
    f('phone').href = 'tel:' + phone.replace(/[^0-9+]/g, '');
    show(section('phone'), phone);

    var url = val('linkedin_url');
    var ok = /^https?:\/\//i.test(url);
    f('linkedin').href = ok ? url : '#';
    show(section('linkedin'), ok);

    var chosen = form.querySelector('input[name="template"]:checked');
    if (chosen) card.className = 'tp tp--' + chosen.value;
  }

  form.addEventListener('input', update);
  form.addEventListener('change', update);
  update();
})();

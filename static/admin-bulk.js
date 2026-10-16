// Массовый выбор строк в админских таблицах: «выбрать все», счётчик
// выбранных и кнопки действий, неактивные, пока ничего не отмечено.
// Разметка: контейнер [data-bulk] с .bulk-all, .bulk-item, .bulk-count и
// .bulk-submit внутри.
document.querySelectorAll('[data-bulk]').forEach(root => {
  const all = root.querySelector('.bulk-all');
  const items = [...root.querySelectorAll('.bulk-item')];
  const count = root.querySelector('.bulk-count');
  const submits = root.querySelectorAll('.bulk-submit');

  function update() {
    const n = items.filter(i => i.checked).length;
    if (count) count.textContent = n ? 'Выбрано: ' + n : 'Ничего не выбрано';
    submits.forEach(b => { b.disabled = n === 0; });
    if (all) {
      all.checked = n > 0 && n === items.length;
      all.indeterminate = n > 0 && n < items.length;
    }
  }

  all?.addEventListener('change', () => {
    items.forEach(i => { i.checked = all.checked; });
    update();
  });
  items.forEach(i => i.addEventListener('change', update));
  update();
});

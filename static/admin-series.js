function initSeriesDraggable() {
  const list = document.getElementById('series-list');
  if (!list) return;
  const saver = createOrderSaver({ url: '/admin/series/reorder', list, attr: 'seriesId' });

  let dragEl = null;

  list.querySelectorAll('[data-series-id]').forEach(row => {
    row.addEventListener('dragstart', e => {
      dragEl = row;
      e.dataTransfer.effectAllowed = 'move';
    });

    row.addEventListener('dragover', e => {
      e.preventDefault();
      if (!dragEl || dragEl === row) return;
      const rect = row.getBoundingClientRect();
      const before = e.clientY - rect.top < rect.height / 2;
      row.parentNode.insertBefore(dragEl, before ? row : row.nextSibling);
    });

    row.addEventListener('dragend', () => {
      dragEl = null;
      saver.save();
    });
  });

  // ↑/↓ buttons: the same reorder from the keyboard or a touch screen.
  list.addEventListener('click', e => {
    const btn = e.target.closest('[data-move]');
    if (!btn) return;
    const row = btn.closest('[data-series-id]');
    if (moveItem(row, Number(btn.dataset.move), 'seriesId')) {
      saver.save();
      btn.focus();
    }
  });
}

initSeriesDraggable();

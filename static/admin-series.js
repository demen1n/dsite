function initSeriesDraggable() {
  const list = document.getElementById('series-list');
  if (!list) return;

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
      saveSeriesOrder();
    });
  });
}

function saveSeriesOrder() {
  const ids = [...document.querySelectorAll('#series-list [data-series-id]')]
    .map(el => el.dataset.seriesId);
  fetch('/admin/series/reorder', {
    method: 'POST',
    headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
    body: ids.map(id => `ids=${id}`).join('&'),
  });
}

initSeriesDraggable();

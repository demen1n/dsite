// Saving a manually set order (drag-and-drop or the move buttons) of the
// admin gallery and series lists.
//
// The list is rearranged in the DOM first, so the save has to report back:
// requests go one at a time (a reorder made while one is in flight is sent
// after it, with the order as it is by then), the response status is
// checked, and if a save fails the list snaps back to the last order the
// server confirmed, with an error instead of a silently "saved" order that
// would vanish on reload.
//
//   const saver = createOrderSaver({ url, list, attr });
//   ... rearrange list children ...; saver.save();
//
// list — the element whose children carry the ids, attr — the dataset key
// of the id (e.g. 'photoId' for data-photo-id).
function createOrderSaver({ url, list, attr }) {
  const items = () => [...list.children].filter(el => el.dataset[attr]);
  const currentIds = () => items().map(el => el.dataset[attr]);

  let saved = currentIds();
  let inFlight = false, pending = false;

  let status = list.parentNode.querySelector(':scope > .order-status');
  if (!status) {
    status = document.createElement('p');
    status.className = 'order-status';
    status.setAttribute('role', 'status');
    status.style.cssText = 'font-size:0.82rem; margin:0.5rem 0 0; min-height:1.2em;';
    list.after(status);
  }
  const show = (text, isError) => {
    status.textContent = text;
    status.style.color = isError ? 'var(--danger, #dc2626)' : 'var(--muted)';
  };

  function restore(ids) {
    const byId = new Map(items().map(el => [el.dataset[attr], el]));
    ids.forEach(id => { const el = byId.get(id); if (el) list.appendChild(el); });
  }

  async function send() {
    inFlight = true;
    const ids = currentIds();
    show('Сохраняю порядок…');
    try {
      const resp = await fetch(url, {
        method: 'POST',
        headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
        body: ids.map(id => `ids=${encodeURIComponent(id)}`).join('&'),
      });
      if (!resp.ok) throw new Error(`HTTP ${resp.status}`);
      saved = ids;
      show('Порядок сохранён');
    } catch (err) {
      pending = false;
      restore(saved);
      show(`Не удалось сохранить порядок (${err.message}) — вернул как было. Попробуйте ещё раз.`, true);
    }
    inFlight = false;
    if (pending) { pending = false; send(); }
  }

  return {
    save() {
      if (inFlight) { pending = true; return; }
      send();
    },
  };
}

// moveItem shifts el one position up/left (dir -1) or down/right (+1) among
// its siblings that carry attr; returns whether it moved.
function moveItem(el, dir, attr) {
  const sib = dir < 0 ? el.previousElementSibling : el.nextElementSibling;
  if (!sib || !sib.dataset[attr]) return false;
  if (dir < 0) sib.before(el); else sib.after(el);
  return true;
}

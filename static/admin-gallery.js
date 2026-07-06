async function resizeImage(file, maxWidth, quality) {
  const bitmap = await createImageBitmap(file, { imageOrientation: 'from-image' });
  const scale = Math.min(1, maxWidth / bitmap.width);
  const w = Math.round(bitmap.width * scale);
  const h = Math.round(bitmap.height * scale);
  // Use regular canvas — Safari's OffscreenCanvas silently falls back to PNG for WebP encoding
  const canvas = document.createElement('canvas');
  canvas.width = w;
  canvas.height = h;
  const ctx = canvas.getContext('2d');
  ctx.imageSmoothingEnabled = true;
  ctx.imageSmoothingQuality = 'high';
  ctx.drawImage(bitmap, 0, 0, w, h);
  const blob = await new Promise(resolve => canvas.toBlob(resolve, 'image/webp', quality));
  if (blob.type !== 'image/webp') {
    const fallback = await new Promise(resolve => canvas.toBlob(resolve, 'image/jpeg', quality));
    return { blob: fallback, w, h, ext: '.jpg' };
  }
  return { blob, w, h, ext: '.webp' };
}

async function uploadPhotos() {
  const input = document.getElementById('photo-input');
  const caption = document.getElementById('photo-caption').value;
  const catEl = document.getElementById('photo-category');
  const categoryId = catEl ? catEl.value : '0';
  const placeEl = document.getElementById('photo-place');
  const placeId = placeEl ? placeEl.value : '0';
  const progress = document.getElementById('upload-progress');
  const btn = document.getElementById('upload-btn');

  if (!input.files.length) {
    progress.textContent = 'Выберите файлы';
    return;
  }

  btn.disabled = true;
  btn.textContent = 'Обработка…';

  const files = [...input.files];
  const total = files.length;
  let resizeDone = 0;

  progress.textContent = `Ресайз 0/${total}…`;
  const results = await Promise.all(files.map(async (file) => {
    const result = await resizeImage(file, 1600, 0.85);
    resizeDone++;
    progress.textContent = `Ресайз ${resizeDone}/${total}…`;
    return result;
  }));

  const form = new FormData();
  form.append('caption', caption);
  form.append('category_id', categoryId);
  form.append('place_id', placeId);
  results.forEach(({ blob, w, h, ext }, i) => {
    form.append('photos', new File([blob], files[i].name.replace(/\.\w+$/, ext), { type: blob.type }));
    form.append('widths[]', w);
    form.append('heights[]', h);
  });

  await new Promise((resolve, reject) => {
    const xhr = new XMLHttpRequest();
    xhr.open('POST', '/admin/gallery/upload');
    xhr.upload.onprogress = e => {
      if (e.lengthComputable) {
        const pct = Math.round(e.loaded / e.total * 100);
        progress.textContent = `Загрузка ${pct}%…`;
      }
    };
    xhr.onload = () => {
      if (xhr.status === 200) {
        document.getElementById('gallery-list').innerHTML = xhr.responseText;
        initDraggable();
        input.value = '';
        document.getElementById('photo-caption').value = '';
        progress.textContent = `✓ Загружено ${total} фото`;
        btn.textContent = 'Загрузить';
        btn.disabled = false;
        resolve();
      } else {
        progress.textContent = 'Ошибка загрузки';
        btn.disabled = false;
        btn.textContent = 'Загрузить';
        reject();
      }
    };
    xhr.onerror = () => {
      progress.textContent = 'Ошибка загрузки';
      btn.disabled = false;
      btn.textContent = 'Загрузить';
      reject();
    };
    xhr.send(form);
  });
}

const selected = new Set();

function getSelectionBar() {
  let bar = document.getElementById('selection-bar');
  if (!bar) {
    bar = document.createElement('div');
    bar.id = 'selection-bar';
    bar.style.cssText = 'display:none; align-items:center; gap:0.75rem; background:var(--surface); border:1px solid var(--border); border-radius:var(--radius); padding:0.5rem 1rem; margin-bottom:0.75rem; font-size:0.88rem;';
    bar.innerHTML = '<span id="sel-count"></span><button onclick="clearSelection()" style="background:none;border:none;color:var(--muted);cursor:pointer;padding:0;font-size:0.82rem;">Снять выделение</button>';
    const grid = document.getElementById('gallery-list-grid');
    if (grid) grid.parentNode.insertBefore(bar, grid);
  }
  return bar;
}

function updateSelectionBar() {
  const bar = getSelectionBar();
  if (selected.size > 0) {
    bar.style.display = 'flex';
    document.getElementById('sel-count').textContent = `Выбрано: ${selected.size}`;
  } else {
    bar.style.display = 'none';
  }
}

function clearSelection() {
  selected.clear();
  document.querySelectorAll('[data-photo-id].photo-sel').forEach(el => el.classList.remove('photo-sel'));
  updateSelectionBar();
}

function toggleSelect(el) {
  const id = el.dataset.photoId;
  if (selected.has(id)) {
    selected.delete(id);
    el.classList.remove('photo-sel');
  } else {
    selected.add(id);
    el.classList.add('photo-sel');
  }
  updateSelectionBar();
}

function initDraggable() {
  clearSelection();
  document.querySelectorAll('[data-photo-id]').forEach(el => {
    if (!el.querySelector('.photo-check')) {
      const chk = document.createElement('div');
      chk.className = 'photo-check';
      chk.textContent = '✓';
      chk.style.cssText = 'position:absolute;top:6px;left:6px;width:20px;height:20px;border-radius:50%;background:var(--accent,#555);color:#fff;font-size:11px;display:none;align-items:center;justify-content:center;pointer-events:none;z-index:2;';
      el.appendChild(chk);
    }

    el.setAttribute('draggable', 'true');

    el.addEventListener('click', e => {
      if (e.target.closest('button, select, form')) return;
      toggleSelect(el);
    });

    el.addEventListener('dragstart', e => {
      if (!selected.has(el.dataset.photoId)) {
        clearSelection();
        selected.add(el.dataset.photoId);
        el.classList.add('photo-sel');
        updateSelectionBar();
      }
      e.dataTransfer.effectAllowed = 'move';
      if (selected.size > 1) {
        const badge = document.createElement('div');
        badge.style.cssText = 'position:fixed;top:-100px;padding:4px 12px;background:var(--accent,#555);color:#fff;border-radius:20px;font-size:13px;font-family:sans-serif;white-space:nowrap;';
        badge.textContent = `${selected.size} фото`;
        document.body.appendChild(badge);
        e.dataTransfer.setDragImage(badge, 40, 16);
        setTimeout(() => badge.remove(), 0);
      }
    });

    el.addEventListener('dragover', e => {
      e.preventDefault();
      if (!selected.has(el.dataset.photoId)) {
        el.style.outline = '2px dashed var(--accent,#555)';
        el.style.outlineOffset = '-2px';
      }
    });

    el.addEventListener('dragleave', () => {
      el.style.outline = '';
      el.style.outlineOffset = '';
    });

    el.addEventListener('drop', e => {
      e.preventDefault();
      el.style.outline = '';
      el.style.outlineOffset = '';
      if (selected.has(el.dataset.photoId)) return;

      const grid = el.parentNode;
      const allItems = [...grid.children];
      const selectedEls = allItems.filter(item => item.dataset?.photoId && selected.has(item.dataset.photoId));
      if (!selectedEls.length) return;

      const movingForward = allItems.indexOf(selectedEls[0]) < allItems.indexOf(el);

      selectedEls.forEach(s => s.remove());
      if (movingForward) {
        let anchor = el;
        selectedEls.forEach(s => { anchor.after(s); anchor = s; });
      } else {
        selectedEls.forEach(s => grid.insertBefore(s, el));
      }

      clearSelection();
      saveOrder();
    });
  });

  const grid = document.getElementById('gallery-list-grid');
  if (grid) {
    grid.addEventListener('click', e => {
      if (!e.target.closest('[data-photo-id]')) clearSelection();
    });
  }
}

document.addEventListener('keydown', e => { if (e.key === 'Escape') clearSelection(); });

function saveOrder() {
  const ids = [...document.querySelectorAll('[data-photo-id]')]
    .map(el => el.dataset.photoId);
  fetch('/admin/gallery/reorder', {
    method: 'POST',
    headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
    body: ids.map(id => `ids=${id}`).join('&'),
  });
}

document.body.addEventListener('htmx:afterSwap', e => {
  const id = e.detail.target.id;
  if (id === 'gallery-list' || id === 'gallery-main') initDraggable();
});

initDraggable();

function autoResize(el) {
  el.style.height = 'auto';
  el.style.height = el.scrollHeight + 'px';
}
autoResize(document.getElementById('title'));

function updatePublishLabel() {
  const checked = document.getElementById('published-toggle').checked;
  document.getElementById('publish-label').textContent = checked ? 'Опубликован' : 'Черновик';
}

document.getElementById('post-form').addEventListener('submit', async function(e) {
  const input = document.getElementById('cover-input');
  if (!input.files.length) return;
  e.preventDefault();
  const file = input.files[0];
  const resized = await resizeImage(file, 1600, 0.65);
  const dt = new DataTransfer();
  dt.items.add(new File([resized], file.name.replace(/\.\w+$/, '.webp'), { type: 'image/webp' }));
  input.files = dt.files;
  this.submit();
});

async function resizeImage(file, maxWidth, quality) {
  const bitmap = await createImageBitmap(file, { imageOrientation: 'from-image' });
  const scale = Math.min(1, maxWidth / bitmap.width);
  const w = Math.round(bitmap.width * scale), h = Math.round(bitmap.height * scale);
  // Use regular canvas — Safari's OffscreenCanvas silently falls back to PNG for WebP encoding
  const canvas = document.createElement('canvas');
  canvas.width = w;
  canvas.height = h;
  canvas.getContext('2d').drawImage(bitmap, 0, 0, w, h);
  const blob = await new Promise(resolve => canvas.toBlob(resolve, 'image/webp', quality));
  if (blob.type !== 'image/webp') {
    return new Promise(resolve => canvas.toBlob(resolve, 'image/jpeg', quality));
  }
  return blob;
}

function previewCover(input) {
  if (!input.files.length) return;
  const thumb = document.getElementById('cover-thumb');
  thumb.src = URL.createObjectURL(input.files[0]);
  thumb.style.display = 'block';
}

function removeCover() {
  document.getElementById('cover-input').value = '';
  document.getElementById('remove-cover-flag').value = '1';
  const thumb = document.getElementById('cover-thumb');
  thumb.src = ''; thumb.style.display = 'none';
}

let slugEdited = false;
document.getElementById('slug').addEventListener('input', () => { slugEdited = true; });
function autoSlug(val) {
  if (slugEdited) return;
  document.getElementById('slug').value = val.toLowerCase()
    .replace(/[^a-z0-9\s-]/g, '').trim().replace(/\s+/g, '-');
}

const bodyTA = document.getElementById('body');
bodyTA.addEventListener('dragover', e => { e.preventDefault(); bodyTA.style.outline = '2px solid var(--accent)'; });
bodyTA.addEventListener('dragleave', () => { bodyTA.style.outline = ''; });
bodyTA.addEventListener('drop', async e => {
  e.preventDefault(); bodyTA.style.outline = '';
  const file = e.dataTransfer.files[0];
  if (!file || !file.type.startsWith('image/')) return;

  const placeholder = '![загрузка…]()';
  const start = bodyTA.selectionStart;
  bodyTA.value = bodyTA.value.substring(0, start) + placeholder + bodyTA.value.substring(start);
  bodyTA.selectionStart = bodyTA.selectionEnd = start + placeholder.length;

  let resized;
  try {
    resized = await resizeImage(file, 1600, 0.65);
  } catch (err) {
    bodyTA.value = bodyTA.value.replace(placeholder, '');
    alert('Не удалось обработать изображение.\nВозможно, формат не поддерживается (например, HEIC).\nКонвертируйте в JPG или PNG и попробуйте снова.');
    return;
  }

  const fd = new FormData();
  const ext = resized.type === 'image/webp' ? '.webp' : '.jpg';
  fd.append('image', new File([resized], file.name.replace(/\.\w+$/, ext), { type: resized.type }));
  const url = await new Promise((resolve, reject) => {
    const xhr = new XMLHttpRequest();
    xhr.open('POST', '/admin/upload');
    xhr.upload.onprogress = e => {
      if (e.lengthComputable) {
        console.log(`Загрузка: ${Math.round(e.loaded / e.total * 100)}%`);
      }
    };
    xhr.onload = () => xhr.status === 200 ? resolve(xhr.responseText) : reject(xhr.status);
    xhr.onerror = () => reject('network error');
    xhr.send(fd);
  }).catch(err => {
    bodyTA.value = bodyTA.value.replace(placeholder, '');
    alert('Ошибка загрузки на сервер: ' + err);
    return null;
  });
  if (!url) return;
  const md = `![](${url})`;
  bodyTA.value = bodyTA.value.replace(placeholder, md);
  bodyTA.selectionStart = bodyTA.selectionEnd = bodyTA.value.indexOf(md) + md.length;
  htmx.trigger(bodyTA, 'input');
});

function insertMD(before, after) {
  const ta = document.getElementById('body');
  const start = ta.selectionStart, end = ta.selectionEnd;
  const sel = ta.value.substring(start, end);
  ta.value = ta.value.substring(0, start) + before + sel + after + ta.value.substring(end);
  ta.selectionStart = start + before.length;
  ta.selectionEnd = end + before.length;
  ta.focus();
  htmx.trigger(ta, 'input');
}

document.getElementById('preview').addEventListener('click', e => {
  const img = e.target.closest('img');
  if (!img || !img.src.includes('/uploads/')) return;
  const filename = img.src.split('/uploads/').pop();
  document.getElementById('itg-filename').value = filename;
  document.getElementById('itg-caption').value = img.alt || '';
  document.getElementById('img-popup-result').textContent = '';
  const popup = document.getElementById('img-popup');
  popup.style.display = 'block';
  popup.style.left = Math.min(e.clientX, window.innerWidth - 280) + 'px';
  popup.style.top = Math.min(e.clientY + 12, window.innerHeight - 120) + 'px';
});

function closeImgPopup() {
  document.getElementById('img-popup').style.display = 'none';
}

function afterImgToGallery() {
  setTimeout(closeImgPopup, 1500);
}

document.addEventListener('keydown', e => {
  if (e.key === 'Escape') closeImgPopup();
});

const DRAFT_KEY = window.DRAFT_KEY;
let autoSaveTimer;

(function draftLoad() {
  const saved = localStorage.getItem(DRAFT_KEY);
  if (!saved) return;
  let draft;
  try { draft = JSON.parse(saved); } catch(e) { return; }

  const titleEl = document.getElementById('title');
  const bodyEl  = document.getElementById('body');
  const tagsEl  = document.getElementById('tags');

  if (draft.title === titleEl.value && draft.body === bodyEl.value && draft.tags === tagsEl.value) return;

  const bar = document.createElement('div');
  bar.id = 'autosave-bar';
  bar.style.cssText = 'background:#fef9c3;border-bottom:1px solid #fde047;padding:0.45rem 1.5rem;font-size:0.83rem;font-family:var(--sans);display:flex;align-items:center;gap:0.75rem;flex-shrink:0;';
  bar.innerHTML = `<span>Несохранённый черновик от ${new Date(draft.ts).toLocaleString('ru')}</span>`
    + `<button type="button" onclick="draftRestore()" style="border:1px solid #ca8a04;background:none;padding:0.15rem 0.55rem;border-radius:3px;cursor:pointer;font-size:0.8rem;">Восстановить</button>`
    + `<button type="button" onclick="draftDiscard()" style="border:none;background:none;color:#92400e;cursor:pointer;font-size:0.8rem;">Удалить</button>`;

  const wrap = document.querySelector('.editor-wrap');
  const meta = document.querySelector('.editor-meta');
  wrap.insertBefore(bar, meta);
  window._pendingDraft = draft;
})();

function draftRestore() {
  const draft = window._pendingDraft;
  if (!draft) return;
  document.getElementById('title').value = draft.title;
  document.getElementById('body').value  = draft.body;
  document.getElementById('tags').value  = draft.tags;
  autoResize(document.getElementById('title'));
  htmx.trigger(document.getElementById('body'), 'input');
  document.getElementById('autosave-bar')?.remove();
}

function draftDiscard() {
  localStorage.removeItem(DRAFT_KEY);
  document.getElementById('autosave-bar')?.remove();
}

function draftSave() {
  localStorage.setItem(DRAFT_KEY, JSON.stringify({
    title: document.getElementById('title').value,
    body:  document.getElementById('body').value,
    tags:  document.getElementById('tags').value,
    ts:    Date.now(),
  }));
}

['title', 'body', 'tags'].forEach(id => {
  document.getElementById(id)?.addEventListener('input', () => {
    clearTimeout(autoSaveTimer);
    autoSaveTimer = setTimeout(draftSave, 2000);
  });
});

document.getElementById('post-form').addEventListener('submit', () => {
  localStorage.removeItem(DRAFT_KEY);
});

let pickerLoaded = false;
function togglePicker() {
  const panel = document.getElementById('picker-panel');
  panel.classList.toggle('open');
  if (panel.classList.contains('open') && !pickerLoaded) {
    pickerLoaded = true;
    htmx.ajax('GET', '/admin/media/picker', { target: '#picker-content', swap: 'innerHTML' });
  }
}

document.addEventListener('click', e => {
  const photo = e.target.closest('.picker-photo');
  if (!photo) return;
  const caption = photo.dataset.caption || '';
  const url = photo.dataset.url;
  insertMD(`![${caption}](${url})`, '');
  document.getElementById('picker-panel').classList.remove('open');
});

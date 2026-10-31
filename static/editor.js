// Ресайз/сжатие картинок теперь делает сервер (internal/imgproc) — качественнее
// и одинаково во всех браузерах. Старый ресайз в браузере оставлен ниже как
// аварийный вариант: поставьте true, если серверная обработка вдруг сломается.
const CLIENT_SIDE_RESIZE = false;

function autoResize(el) {
  el.style.height = 'auto';
  el.style.height = el.scrollHeight + 'px';
}
autoResize(document.getElementById('title'));

function updatePublishLabel() {
  const checked = document.getElementById('published-toggle').checked;
  const at = document.getElementById('published-at').value;
  const scheduled = checked && at && new Date(at) > new Date();
  document.getElementById('publish-label').textContent =
    scheduled ? 'Запланирован' : checked ? 'Опубликован' : 'Черновик';
}

// Дата публикации: сервер хранит и принимает UTC (RFC 3339, скрытое поле
// published_at), а человек видит и правит её в локальном времени
// (datetime-local) — переводим туда-обратно здесь, где известен часовой пояс.
// Show the hidden UTC value in the datetime-local field.
function syncPublishedAtLocal() {
  const local = document.getElementById('published-at-local');
  const hidden = document.getElementById('published-at');
  if (!local || !hidden) return;
  if (!hidden.value) { local.value = ''; return; }
  const d = new Date(hidden.value);
  const pad = n => String(n).padStart(2, '0');
  local.value = d.getFullYear() + '-' + pad(d.getMonth() + 1) + '-' + pad(d.getDate()) +
    'T' + pad(d.getHours()) + ':' + pad(d.getMinutes());
}

(function publishedAtInit() {
  const local = document.getElementById('published-at-local');
  const hidden = document.getElementById('published-at');
  if (!local || !hidden) return;
  syncPublishedAtLocal();
  local.addEventListener('change', () => {
    // Пустое поле — дату не трогаем (сервер оставит текущую).
    hidden.value = local.value ? new Date(local.value).toISOString() : '';
    updatePublishLabel();
  });
})();

document.getElementById('post-form').addEventListener('submit', async function(e) {
  const input = document.getElementById('cover-input');
  if (!input.files.length || !CLIENT_SIDE_RESIZE) return;
  e.preventDefault();
  const file = input.files[0];
  const resized = await resizeImage(file, 1600, 0.8);
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
  const ctx = canvas.getContext('2d');
  ctx.imageSmoothingEnabled = true;
  ctx.imageSmoothingQuality = 'high';
  ctx.drawImage(bitmap, 0, 0, w, h);
  const blob = await new Promise(resolve => canvas.toBlob(resolve, 'image/webp', quality));
  if (blob.type !== 'image/webp') {
    return new Promise(resolve => canvas.toBlob(resolve, 'image/jpeg', quality));
  }
  return blob;
}

// До какой ширины браузер уменьшает картинку, которую сервер отверг как
// слишком тяжёлую (413), — с запасом над итоговыми 1600px сервера.
const OVERSIZE_RESIZE_WIDTH = 3200;

// Браузерный ресайз — только аварийный путь для снимков крупнее 24 Мп
// (6048×4024, дефолт никоновских камер): всё, что не больше, сервер обязан
// обработать сам (и обрабатывает в любой кодировке) — качественнее canvas.
const BROWSER_RESIZE_MIN_PIXELS = 6048 * 4024;

async function exceedsServerSize(file) {
  const bitmap = await createImageBitmap(file);
  const big = bitmap.width * bitmap.height > BROWSER_RESIZE_MIN_PIXELS;
  bitmap.close();
  return big;
}

function postImage(file) {
  const fd = new FormData();
  fd.append('image', file);
  return new Promise((resolve, reject) => {
    const xhr = new XMLHttpRequest();
    xhr.open('POST', '/admin/upload');
    xhr.onload = () => resolve({ status: xhr.status, text: xhr.responseText });
    xhr.onerror = () => reject('нет связи с сервером');
    xhr.send(fd);
  });
}

function uploadErrorText(status, text) {
  if (status >= 502 && status <= 504) {
    return `сервер не ответил (${status}) — возможно, он упал или перезапускался во время обработки`;
  }
  text = (text || '').trim();
  return text ? `${status}: ${text}` : String(status);
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

// Держим в синхронизации с cyrillicMap в internal/handlers/helpers.go
const CYRILLIC_MAP = {
  'а': 'a', 'б': 'b', 'в': 'v', 'г': 'g', 'д': 'd', 'е': 'e', 'ё': 'yo',
  'ж': 'zh', 'з': 'z', 'и': 'i', 'й': 'j', 'к': 'k', 'л': 'l', 'м': 'm',
  'н': 'n', 'о': 'o', 'п': 'p', 'р': 'r', 'с': 's', 'т': 't', 'у': 'u',
  'ф': 'f', 'х': 'kh', 'ц': 'ts', 'ч': 'ch', 'ш': 'sh', 'щ': 'shch',
  'ъ': '', 'ы': 'y', 'ь': '', 'э': 'e', 'ю': 'yu', 'я': 'ya',
};
function transliterate(s) {
  return [...s].map(ch => CYRILLIC_MAP[ch] ?? ch).join('');
}
function autoSlug(val) {
  if (slugEdited) return;
  document.getElementById('slug').value = transliterate(val.toLowerCase())
    .replace(/[^a-z0-9\s-]/g, '').trim().replace(/\s+/g, '-');
}

const bodyTA = document.getElementById('body');
bodyTA.addEventListener('dragover', e => { e.preventDefault(); bodyTA.style.outline = '2px solid var(--accent)'; });
bodyTA.addEventListener('dragleave', () => { bodyTA.style.outline = ''; });
bodyTA.addEventListener('drop', async e => {
  e.preventDefault(); bodyTA.style.outline = '';
  const files = [...e.dataTransfer.files].filter(f => f.type.startsWith('image/'));
  if (!files.length) return;

  for (const file of files) {
    const placeholder = `![загрузка…]()`;
    const start = bodyTA.selectionStart;
    bodyTA.value = bodyTA.value.substring(0, start) + placeholder + bodyTA.value.substring(start);
    bodyTA.selectionStart = bodyTA.selectionEnd = start + placeholder.length;

    let uploadFile = file;
    if (CLIENT_SIDE_RESIZE) {
      let resized;
      try {
        resized = await resizeImage(file, 1600, 0.8);
      } catch {
        bodyTA.value = bodyTA.value.replace(placeholder, '');
        alert('Не удалось обработать изображение.\nВозможно, формат не поддерживается (например, HEIC).\nКонвертируйте в JPG или PNG и попробуйте снова.');
        continue;
      }
      const ext = resized.type === 'image/webp' ? '.webp' : '.jpg';
      uploadFile = new File([resized], file.name.replace(/\.\w+$/, ext), { type: resized.type });
    }

    let url = null;
    try {
      let res = await postImage(uploadFile);
      if (res.status === 413 && await exceedsServerSize(file)) {
        // Сервер не берётся декодировать такой огромный файл — уменьшаем его
        // в браузере и отправляем ещё раз; финальную обработку всё равно
        // сделает сервер.
        const resized = await resizeImage(file, OVERSIZE_RESIZE_WIDTH, 0.92);
        const ext = resized.type === 'image/webp' ? '.webp' : '.jpg';
        res = await postImage(new File([resized], file.name.replace(/\.\w+$/, ext), { type: resized.type }));
      }
      if (res.status !== 200) throw uploadErrorText(res.status, res.text);
      url = res.text;
    } catch (err) {
      bodyTA.value = bodyTA.value.replace(placeholder, '');
      alert('Ошибка загрузки на сервер: ' + err);
    }
    if (!url) continue;
    const md = `![](${url})\n`;
    bodyTA.value = bodyTA.value.replace(placeholder, md);
    htmx.trigger(bodyTA, 'input');
  }
  bodyTA.selectionStart = bodyTA.selectionEnd = bodyTA.value.length;
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

// Находит диапазон `![...](src)` в тексте поста, соответствующий n-му
// вхождению этого src среди <img> превью — так отличаем повторно
// использованную картинку от её же дублей.
function locateImageRange(img) {
  const src = img.getAttribute('src');
  if (!src) return null;
  const allImgs = [...document.querySelectorAll('#preview img')];
  let ordinal = 0;
  for (const el of allImgs) {
    if (el.getAttribute('src') === src) {
      ordinal++;
      if (el === img) break;
    }
  }
  const re = /!\[[^\]]*\]\(([^)\s]+)(?:\s+"[^"]*")?\)/g;
  const md = bodyTA.value;
  let m, seen = 0;
  while ((m = re.exec(md))) {
    if (m[1] === src) {
      seen++;
      if (seen === ordinal) return { start: m.index, end: m.index + m[0].length };
    }
  }
  return null;
}

document.getElementById('preview').addEventListener('click', e => {
  const img = e.target.closest('img');
  if (!img || !img.src.includes('/uploads/')) return;
  const filename = img.src.split('/uploads/').pop();
  document.getElementById('itg-filename').value = filename;
  document.getElementById('itg-caption').value = img.alt || '';
  document.getElementById('img-popup-result').textContent = '';

  const range = locateImageRange(img);
  window._imgPopupRange = range;
  document.getElementById('img-popup-delete').disabled = !range;
  if (range) {
    bodyTA.focus();
    bodyTA.setSelectionRange(range.start, range.end);
  }

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

function deleteImgFromBody() {
  const range = window._imgPopupRange;
  if (!range) return;
  const before = bodyTA.value.slice(0, range.start);
  let after = bodyTA.value.slice(range.end);
  if (before.endsWith('\n') && after.startsWith('\n')) after = after.slice(1);
  bodyTA.value = before + after;
  htmx.trigger(bodyTA, 'input');
  closeImgPopup();
}

document.addEventListener('keydown', e => {
  if (e.key === 'Escape') closeImgPopup();
});

const DRAFT_KEY = window.DRAFT_KEY;
const NEW_DRAFT_KEY = 'dsite_editor_new';
let autoSaveTimer;

// localStorage can throw (private mode, quota, blocked storage) — the draft
// is a best-effort safety net, so never let that break the editor.
function draftGet(key) {
  try { return JSON.parse(localStorage.getItem(key)); } catch { return null; }
}
function draftDel(key) {
  try { localStorage.removeItem(key); } catch { /* ignore */ }
}

// The form's current values — what draftSave stores.
function formState() {
  return {
    title:       document.getElementById('title').value,
    body:        document.getElementById('body').value,
    tags:        document.getElementById('tags-hidden').value,
    slug:        document.getElementById('slug').value,
    seriesID:    document.getElementById('series-id').value,
    publishedAt: document.getElementById('published-at').value,
    published:   document.getElementById('published-toggle').checked,
  };
}

// What the server rendered into the form, i.e. what is actually saved.
// default*, not the live values: the browser may have restored edits into
// the fields on reload.
function savedState() {
  const series = [...document.getElementById('series-id').options].find(o => o.defaultSelected);
  return {
    title:       document.getElementById('title').defaultValue,
    body:        document.getElementById('body').defaultValue,
    tags:        document.getElementById('tags-hidden').value,
    slug:        document.getElementById('slug').defaultValue,
    seriesID:    series ? series.value : '0',
    publishedAt: document.getElementById('published-at').defaultValue,
    published:   document.getElementById('published-toggle').defaultChecked,
  };
}

// The server's slugify (helpers.go), so a typed slug compares equal to the
// one the server stored from it.
function slugNorm(s) {
  return transliterate((s || '').toLowerCase())
    .replace(/[\s_]/g, '-').replace(/[^a-z0-9-]/g, '')
    .replace(/-+/g, '-').replace(/^-|-$/g, '');
}

// Fields a draft from before they were stored (undefined) don't count. The
// server normalizes the slug and fills in the date on first publish, so an
// empty draft value there matches anything.
function sameContent(draft, state) {
  const norm = s => (s || '').replace(/\r\n/g, '\n').trim();
  const opt = (v, eq) => v === undefined || eq(v);
  return norm(draft.title) === norm(state.title)
    && norm(draft.body) === norm(state.body)
    && (draft.tags || '') === (state.tags || '')
    && opt(draft.slug, v => !slugNorm(v) || slugNorm(v) === slugNorm(state.slug))
    && opt(draft.seriesID, v => v === state.seriesID)
    && opt(draft.publishedAt, v => !v || Date.parse(v) === Date.parse(state.publishedAt))
    && opt(draft.published, v => v === state.published);
}

(function draftLoad() {
  const saved = savedState();
  // The draft is only dropped once the server has it: on submit it's kept
  // (the save may still fail — 413/500, expired session, lost connection),
  // and cleared here when the page the save redirected to shows the same
  // content. A new post's draft lives under NEW_DRAFT_KEY and lands on the
  // post's own edit page, so check that one too.
  if (DRAFT_KEY !== NEW_DRAFT_KEY) {
    const nd = draftGet(NEW_DRAFT_KEY);
    if (nd && sameContent(nd, saved)) draftDel(NEW_DRAFT_KEY);
  }
  const draft = draftGet(DRAFT_KEY);
  if (!draft) return;
  if (sameContent(draft, saved)) { draftDel(DRAFT_KEY); return; }

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
  document.getElementById('tags-hidden').value  = draft.tags;
  if (draft.slug !== undefined) {
    document.getElementById('slug').value = draft.slug;
    slugEdited = true;
  }
  if (draft.seriesID !== undefined) document.getElementById('series-id').value = draft.seriesID;
  if (draft.publishedAt !== undefined) {
    document.getElementById('published-at').value = draft.publishedAt;
    syncPublishedAtLocal();
  }
  if (draft.published !== undefined) document.getElementById('published-toggle').checked = draft.published;
  updatePublishLabel();
  autoResize(document.getElementById('title'));
  htmx.trigger(document.getElementById('body'), 'input');
  document.getElementById('autosave-bar')?.remove();
}

function draftDiscard() {
  draftDel(DRAFT_KEY);
  document.getElementById('autosave-bar')?.remove();
}

function draftSave() {
  clearTimeout(autoSaveTimer);
  try {
    localStorage.setItem(DRAFT_KEY, JSON.stringify({ ...formState(), ts: Date.now() }));
  } catch { /* storage full or unavailable — nothing better to do */ }
}

function draftSchedule() {
  clearTimeout(autoSaveTimer);
  autoSaveTimer = setTimeout(draftSave, 2000);
}
['title', 'body', 'tags-hidden', 'slug'].forEach(id => {
  document.getElementById(id)?.addEventListener('input', draftSchedule);
});
['series-id', 'published-at-local', 'published-toggle'].forEach(id => {
  document.getElementById(id)?.addEventListener('change', draftSchedule);
});

// Flush the debounced autosave right away instead of dropping the draft:
// whether the save worked is only known on the page it redirects to.
document.getElementById('post-form').addEventListener('submit', () => {
  if (autoSaveTimer) draftSave();
});
window.addEventListener('pagehide', () => {
  if (autoSaveTimer) draftSave();
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

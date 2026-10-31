let _lbSrcs = [], _lbAlts = [], _lbIdx = 0, _lbScrollY = 0, _lbOpener = null;
const _lb = () => document.getElementById('lightbox');
// The page behind the dialog: made inert while it's open, so Tab and screen
// readers stay inside the lightbox.
const _lbBackground = () => document.querySelectorAll('body > header, body > main, body > footer');

// srcs/alts are parallel arrays for the whole set the photo belongs to; alts
// may be omitted (no description then).
function openLightbox(src, srcs, alts) {
  if (srcs) { _lbSrcs = srcs; _lbAlts = alts || []; }
  _lbIdx = _lbSrcs.indexOf(src);
  if (_lbIdx === -1) { _lbSrcs = [src]; _lbAlts = alts ? [alts[0]] : []; _lbIdx = 0; }
  _lbShow(_lbIdx);
  _lbScrollY = window.scrollY;
  _lbOpener = document.activeElement;
  const sbW = window.innerWidth - document.documentElement.clientWidth;
  _lb().classList.add('open');
  _lbBackground().forEach(el => { el.inert = true; });
  document.body.style.overflow = 'hidden';
  if (sbW > 0) document.body.style.paddingRight = sbW + 'px';
  document.getElementById('lightbox-close').focus();
}
function _lbShow(idx) {
  const img = document.getElementById('lightbox-img');
  img.src = _lbSrcs[idx];
  img.alt = _lbAlts[idx] || '';
  const label = _lbSrcs.length > 1 ? `Фото ${idx + 1} из ${_lbSrcs.length}` : 'Фото';
  _lb().setAttribute('aria-label', _lbAlts[idx] ? `${label}: ${_lbAlts[idx]}` : label);
  document.getElementById('lightbox-prev').style.visibility = idx > 0 ? 'visible' : 'hidden';
  document.getElementById('lightbox-next').style.visibility = idx < _lbSrcs.length - 1 ? 'visible' : 'hidden';
}
function stepLightbox(dir) {
  const n = _lbIdx + dir;
  if (n < 0 || n >= _lbSrcs.length) return;
  _lbIdx = n; _lbShow(_lbIdx);
}
function closeLightbox() {
  _lb().classList.remove('open');
  _lbBackground().forEach(el => { el.inert = false; });
  document.body.style.overflow = '';
  document.body.style.paddingRight = '';
  window.scrollTo(0, _lbScrollY);
  // Back to the photo it was opened from, so keyboard users keep their place.
  if (_lbOpener && document.contains(_lbOpener)) _lbOpener.focus({ preventScroll: true });
  _lbOpener = null;
}
_lb().addEventListener('click', e => { if (e.target === e.currentTarget) closeLightbox(); });
document.getElementById('lightbox-close').addEventListener('click', closeLightbox);
document.getElementById('lightbox-prev').addEventListener('click', () => stepLightbox(-1));
document.getElementById('lightbox-next').addEventListener('click', () => stepLightbox(1));
document.addEventListener('keydown', e => {
  if (!_lb().classList.contains('open')) return;
  if (e.key === 'Escape') closeLightbox();
  if (e.key === 'ArrowLeft') stepLightbox(-1);
  if (e.key === 'ArrowRight') stepLightbox(1);
  if (e.key === 'Tab') {
    // Cycle through the dialog's visible buttons (the rest of the page is inert).
    const btns = [..._lb().querySelectorAll('button')].filter(b => b.style.visibility !== 'hidden');
    const i = btns.indexOf(document.activeElement);
    const next = btns[(i + (e.shiftKey ? -1 : 1) + btns.length) % btns.length];
    if (next) { e.preventDefault(); next.focus(); }
  }
});

// Post/series images: clickable, and reachable and openable from the keyboard.
const _lbTargets = '.prose img, [data-lightbox]';
function _lbOpenFrom(img) {
  const imgs = [
    ...document.querySelectorAll('[data-lightbox]'),
    ...document.querySelectorAll('.prose img'),
  ];
  openLightbox(img.src, imgs.map(i => i.src), imgs.map(i => i.alt));
}
document.querySelectorAll(_lbTargets).forEach(img => {
  if (img.closest('a')) return; // a linked image is the link's job
  img.tabIndex = 0;
  img.setAttribute('role', 'button');
  if (!img.getAttribute('aria-label')) img.setAttribute('aria-label', img.alt ? `Открыть фото: ${img.alt}` : 'Открыть фото');
});
document.addEventListener('click', e => {
  const img = e.target.closest(_lbTargets);
  if (img) _lbOpenFrom(img);
});
document.addEventListener('keydown', e => {
  if (e.key !== 'Enter' && e.key !== ' ') return;
  const img = e.target.closest && e.target.closest(_lbTargets);
  if (!img || img.closest('a')) return;
  e.preventDefault();
  _lbOpenFrom(img);
});

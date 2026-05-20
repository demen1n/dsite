let _lbSrcs = [], _lbIdx = 0, _lbScrollY = 0;
function openLightbox(src, srcs) {
  if (srcs) _lbSrcs = srcs;
  _lbIdx = _lbSrcs.indexOf(src);
  if (_lbIdx === -1) { _lbSrcs = [src]; _lbIdx = 0; }
  _lbShow(_lbIdx);
  _lbScrollY = window.scrollY;
  const sbW = window.innerWidth - document.documentElement.clientWidth;
  document.getElementById('lightbox').classList.add('open');
  document.body.style.overflow = 'hidden';
  if (sbW > 0) document.body.style.paddingRight = sbW + 'px';
}
function _lbShow(idx) {
  document.getElementById('lightbox-img').src = _lbSrcs[idx];
  document.getElementById('lightbox-prev').style.visibility = idx > 0 ? 'visible' : 'hidden';
  document.getElementById('lightbox-next').style.visibility = idx < _lbSrcs.length - 1 ? 'visible' : 'hidden';
}
function stepLightbox(dir) {
  const n = _lbIdx + dir;
  if (n < 0 || n >= _lbSrcs.length) return;
  _lbIdx = n; _lbShow(_lbIdx);
}
function closeLightbox() {
  document.getElementById('lightbox').classList.remove('open');
  document.body.style.overflow = '';
  document.body.style.paddingRight = '';
  window.scrollTo(0, _lbScrollY);
}
document.getElementById('lightbox').addEventListener('click', e => { if (e.target === e.currentTarget) closeLightbox(); });
document.addEventListener('keydown', e => {
  if (!document.getElementById('lightbox').classList.contains('open')) return;
  if (e.key === 'Escape') closeLightbox();
  if (e.key === 'ArrowLeft') stepLightbox(-1);
  if (e.key === 'ArrowRight') stepLightbox(1);
});
document.addEventListener('click', e => {
  const img = e.target.closest('.prose img, [data-lightbox]');
  if (!img) return;
  const imgs = [
    ...document.querySelectorAll('[data-lightbox]'),
    ...document.querySelectorAll('.prose img'),
  ];
  openLightbox(img.src, imgs.map(i => i.src));
});

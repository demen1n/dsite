function setupGalleryLightbox() {
  const items = [...document.querySelectorAll('.gallery-item')];
  const srcs = items.map(el => el.querySelector('img').getAttribute('src'));
  items.forEach((item, i) => {
    item.onclick = () => openLightbox(srcs[i], srcs);
  });
}

// packJustified packs items into rows that exactly fill containerWidth,
// each row sharing one height, à la Flickr/Google Photos "justified" grids.
// ratios[i] is item i's width/height. Returns {width, height} per item, in
// the original order — the same "justified gallery" math used for both the
// photo grid and (below) series post-card tiles.
function packJustified(ratios, containerWidth, gap, targetH) {
  const rows = [];
  let row = [], rowRatio = 0;
  ratios.forEach((r, i) => {
    row.push(i);
    rowRatio += r;
    if (rowRatio * targetH >= containerWidth || i === ratios.length - 1) {
      rows.push({ indices: [...row], totalRatio: rowRatio });
      row = []; rowRatio = 0;
    }
  });

  const sized = new Array(ratios.length);
  rows.forEach(({ indices, totalRatio }) => {
    const gapSpace = gap * (indices.length - 1);
    const h = Math.round((containerWidth - gapSpace) / totalRatio);
    let usedWidth = 0;
    indices.forEach((i, j) => {
      const isLastInRow = j === indices.length - 1;
      const w = isLastInRow ? (containerWidth - gapSpace - usedWidth) : Math.round(h * ratios[i]);
      usedWidth += w;
      sized[i] = { width: w, height: h };
    });
  });
  return sized;
}

let galleryRO = null;
function justify() {
  const grid = document.querySelector('.gallery-grid-inner');
  if (!grid) return;
  const items = [...grid.querySelectorAll('.gallery-item')];
  if (!items.length) return;
  const imgs = items.map(el => el.querySelector('img'));

  layout(grid, items, imgs);

  items.forEach((el, i) => {
    if (el.dataset.ratio) return;
    const img = imgs[i];
    if (img.complete && img.naturalWidth) return;
    img.addEventListener('load', () => layout(grid, items, imgs), { once: true });
  });

  if (galleryRO) galleryRO.disconnect();
  let lastW = grid.clientWidth;
  galleryRO = new ResizeObserver(() => {
    const w = grid.clientWidth;
    if (w !== lastW) {
      lastW = w;
      layout(grid, items, imgs);
    }
  });
  galleryRO.observe(grid);
}

function setupFadeIn(imgs) {
  imgs = [...imgs].filter(img => !img.classList.contains('gfade'));
  if (!imgs.length) return;
  const observer = new IntersectionObserver((entries) => {
    entries.forEach(entry => {
      if (!entry.isIntersecting) return;
      entry.target.classList.add('gfade');
      observer.unobserve(entry.target);
    });
  }, { rootMargin: '80px' });
  imgs.forEach(img => {
    if (img.complete && img.naturalWidth) {
      img.classList.add('gfade');
    } else {
      observer.observe(img);
      img.addEventListener('load', () => {
        img.classList.add('gfade');
        observer.unobserve(img);
      }, { once: true });
    }
  });
}

function layout(grid, items, imgs) {
  const W = grid.clientWidth;
  const GAP = 3;
  const TARGET_H = Math.max(160, Math.round(W / 3.5));

  const ratios = items.map((el, i) => {
    if (el.dataset.ratio) {
      const [w, h] = el.dataset.ratio.split('/').map(Number);
      return w && h ? w / h : 1.5;
    }
    const img = imgs[i];
    return img.naturalWidth && img.naturalHeight ? img.naturalWidth / img.naturalHeight : 1.5;
  });

  if (W < 600) {
    items.forEach((item, i) => {
      const h = Math.round(W / ratios[i]);
      item.style.cssText = `width:${W}px; height:${h}px; flex:none; overflow:hidden; cursor:pointer;`;
      imgs[i].style.cssText = `width:100%; height:100%; object-fit:cover; display:block;`;
    });
    setupFadeIn(imgs);
    return;
  }

  const sized = packJustified(ratios, W, GAP, TARGET_H);
  items.forEach((item, i) => {
    item.style.cssText = `width:${sized[i].width}px; height:${sized[i].height}px; flex:none; overflow:hidden; cursor:pointer;`;
    imgs[i].style.cssText = `width:100%; height:100%; object-fit:cover; display:block;`;
  });
  setupFadeIn(imgs);
}

// ── Series post-card tiles: same justified-packing algorithm as the photo
// grid, but only the cover image is sized by it — the title/date footer
// below each card keeps its own natural height. ──
let seriesGridRO = null;
function justifySeriesCards() {
  const grid = document.querySelector('.series-grid');
  if (!grid) return;
  const cards = [...grid.querySelectorAll('.series-card')];
  if (!cards.length) return;
  const covers = cards.map(el => el.querySelector('.series-card-cover'));

  layoutCards(grid, cards, covers);

  covers.forEach(cover => {
    if (cover.tagName !== 'IMG') return;
    if (cover.complete && cover.naturalWidth) return;
    cover.addEventListener('load', () => layoutCards(grid, cards, covers), { once: true });
  });

  if (seriesGridRO) seriesGridRO.disconnect();
  let lastW = grid.clientWidth;
  seriesGridRO = new ResizeObserver(() => {
    const w = grid.clientWidth;
    if (w !== lastW) {
      lastW = w;
      layoutCards(grid, cards, covers);
    }
  });
  seriesGridRO.observe(grid);
}

function layoutCards(grid, cards, covers) {
  const W = grid.clientWidth;
  const GAP = 12;
  const TARGET_H = Math.max(180, Math.round(W / 3.2));

  const ratios = covers.map(cover => {
    if (cover.tagName === 'IMG' && cover.naturalWidth && cover.naturalHeight) {
      return cover.naturalWidth / cover.naturalHeight;
    }
    return 4 / 3; // placeholder (no cover) or not loaded yet
  });

  if (W < 480) {
    cards.forEach((card, i) => {
      card.style.cssText = `width:100%; flex:none;`;
      covers[i].style.height = Math.round(W / ratios[i]) + 'px';
    });
    setupFadeIn(covers.filter(c => c.tagName === 'IMG'));
    return;
  }

  const sized = packJustified(ratios, W, GAP, TARGET_H);
  cards.forEach((card, i) => {
    card.style.cssText = `width:${sized[i].width}px; flex:none;`;
    covers[i].style.height = sized[i].height + 'px';
  });
  setupFadeIn(covers.filter(c => c.tagName === 'IMG'));
}

setupGalleryLightbox();
justify();
justifySeriesCards();

window.addEventListener('load', () => { justify(); justifySeriesCards(); });

let resizeTimer;
window.addEventListener('resize', () => {
  clearTimeout(resizeTimer);
  resizeTimer = setTimeout(() => { justify(); justifySeriesCards(); }, 100);
});

document.body.addEventListener('htmx:afterSwap', e => {
  if (e.detail.target.id === 'gallery-grid') {
    setupGalleryLightbox();
    justify();
    updateFilterActive();
  }
});

function togglePlaces() {
  const btn = document.getElementById('place-toggle');
  const opts = document.getElementById('place-options');
  if (!btn || !opts) return;
  const open = opts.classList.toggle('open');
  btn.classList.toggle('open', open);
}

function closePlaces() {
  const btn = document.getElementById('place-toggle');
  const opts = document.getElementById('place-options');
  if (!btn || !opts) return;
  opts.classList.remove('open');
  btn.classList.remove('open');
}

document.addEventListener('click', e => {
  const btn = document.getElementById('place-toggle');
  const opts = document.getElementById('place-options');
  if (!btn || !opts) return;
  if (btn.contains(e.target)) { togglePlaces(); return; }
  if (!opts.contains(e.target)) closePlaces();
});

function updateFilterActive() {
  const params = new URLSearchParams(window.location.search);
  const cat = params.get('cat') || '';
  const place = params.get('place') || '';

  document.querySelectorAll('.gallery-cats a').forEach(a => {
    const aParams = new URLSearchParams(new URL(a.href).search);
    const aCat = aParams.get('cat') || '';
    a.classList.toggle('active', aCat === cat);
  });

  const opts = document.getElementById('place-options');
  const toggleLabel = document.getElementById('place-toggle-label');
  const toggleBtn = document.getElementById('place-toggle');
  if (opts) {
    opts.querySelectorAll('a').forEach(a => {
      const aParams = new URLSearchParams(new URL(a.href).search);
      const aPlace = aParams.get('place') || '';
      a.classList.toggle('active', aPlace === place);
    });
    if (toggleLabel) {
      const activeLink = opts.querySelector('a.active');
      toggleLabel.textContent = activeLink ? activeLink.textContent : 'Везде';
    }
    if (toggleBtn) toggleBtn.classList.toggle('active', !!place);
  }

  closePlaces();
}

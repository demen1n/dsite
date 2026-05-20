function setupGalleryLightbox() {
  const items = [...document.querySelectorAll('.gallery-item')];
  const srcs = items.map(el => el.querySelector('img').getAttribute('src'));
  items.forEach((item, i) => {
    item.onclick = () => openLightbox(srcs[i], srcs);
  });
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

function setupFadeIn() {
  const imgs = [...document.querySelectorAll('.gallery-item img:not(.gfade)')];
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
    setupFadeIn();
    return;
  }

  const rows = [];
  let row = [], rowRatio = 0;
  ratios.forEach((r, i) => {
    row.push(i);
    rowRatio += r;
    if (rowRatio * TARGET_H >= W || i === ratios.length - 1) {
      rows.push({ indices: [...row], totalRatio: rowRatio });
      row = []; rowRatio = 0;
    }
  });

  rows.forEach(({ indices, totalRatio }) => {
    const gapSpace = GAP * (indices.length - 1);
    const h = Math.round((W - gapSpace) / totalRatio);
    let usedWidth = 0;
    indices.forEach((i, j) => {
      const isLastInRow = j === indices.length - 1;
      const w = isLastInRow ? (W - gapSpace - usedWidth) : Math.round(h * ratios[i]);
      usedWidth += w;
      items[i].style.cssText = `width:${w}px; height:${h}px; flex:none; overflow:hidden; cursor:pointer;`;
      imgs[i].style.cssText = `width:100%; height:100%; object-fit:cover; display:block;`;
    });
  });
  setupFadeIn();
}

setupGalleryLightbox();
justify();

window.addEventListener('load', justify);

let resizeTimer;
window.addEventListener('resize', () => {
  clearTimeout(resizeTimer);
  resizeTimer = setTimeout(justify, 100);
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
  if (!btn.contains(e.target) && !opts.contains(e.target)) closePlaces();
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

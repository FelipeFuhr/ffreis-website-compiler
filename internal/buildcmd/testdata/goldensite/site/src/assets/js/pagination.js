// Progressive-enhancement stub for the paginated projects grid.
(function () {
  var list = document.querySelector('[data-filter-list]');
  if (!list) {
    return;
  }
  list.setAttribute('data-paginated', 'true');
})();

// Polls a submission fragment until grading reaches a terminal state
// (marked by a [data-final] element in the fragment).
document.querySelectorAll("[data-poll]").forEach((el) => {
  const tick = async () => {
    const res = await fetch(el.dataset.poll);
    if (!res.ok) return;
    el.innerHTML = await res.text();
    if (!el.querySelector("[data-final]")) setTimeout(tick, 2000);
  };
  setTimeout(tick, 1500);
});

// Adds a copy button to every pre > code block. Clipboard API only, no dependencies.
(() => {
  const blocks = [...document.querySelectorAll('pre > code')];
  if (!blocks.length) return;
  const status = document.createElement('div');
  status.className = 'sr-only';
  status.setAttribute('role', 'status');
  status.setAttribute('aria-live', 'polite');
  document.body.append(status);
  let timer;
  const say = (message) => {
    clearTimeout(timer);
    status.textContent = message;
    timer = setTimeout(() => { status.textContent = ''; }, 6000);
  };
  blocks.forEach((code, index) => {
    const pre = code.parentElement;
    const text = code.textContent;
    const language = (code.className.match(/language-(\S+)/) || [])[1] || 'text';
    const label = `${language} code block ${index + 1} of ${blocks.length}`;
    const wrap = document.createElement('div');
    wrap.className = 'code-wrap';
    const button = document.createElement('button');
    button.type = 'button';
    button.className = 'copy';
    button.textContent = 'Copy';
    button.setAttribute('aria-label', `Copy ${label}`);
    pre.replaceWith(wrap);
    wrap.append(button, pre);
    let reset;
    button.addEventListener('click', async () => {
      clearTimeout(reset);
      try {
        if (!navigator.clipboard || !navigator.clipboard.writeText) throw new Error('unavailable');
        await navigator.clipboard.writeText(text);
        button.textContent = 'Copied';
        say(`Copied ${label} to the clipboard.`);
        reset = setTimeout(() => { button.textContent = 'Copy'; }, 2000);
      } catch (error) {
        button.textContent = 'Copy';
        say(`Could not copy ${label}. Select the code and copy it manually.`);
      }
    });
  });
})();

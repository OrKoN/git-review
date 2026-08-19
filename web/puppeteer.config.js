import { fileURLToPath } from "node:url";

/** @type {import("puppeteer").Configuration} */
const config = {
  cacheDirectory: fileURLToPath(new URL(".cache/puppeteer", import.meta.url)),
  chrome: {
    skipDownload: true,
  },
  "chrome-headless-shell": {
    skipDownload: false,
  },
  firefox: {
    skipDownload: true,
  },
};

export default config;

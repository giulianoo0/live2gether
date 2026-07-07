module.exports = {
  testDir: "./tests/e2e",
  timeout: 120000,
  use: {
    baseURL: process.env.BASE_URL || "http://127.0.0.1:8080",
    headless: true
  }
};

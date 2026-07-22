exports.redirect = function redirect(url) {
  const error = new Error(`NEXT_REDIRECT: ${url}`);
  error.digest = `NEXT_REDIRECT;replace;${url}`;
  throw error;
};

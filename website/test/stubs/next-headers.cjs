let values = {};

exports.cookies = async function cookies() {
  return {
    get(name) {
      if (!Object.prototype.hasOwnProperty.call(values, name)) {
        return undefined;
      }
      return { name, value: values[name] };
    },
  };
};

exports.__setCookieValues = function setCookieValues(nextValues) {
  values = { ...nextValues };
};

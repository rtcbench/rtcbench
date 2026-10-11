declare module "*.svg" {
  const url: string;
  export default url;
}

declare module "*.tmpl" {
  const source: string;
  export default source;
}

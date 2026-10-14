import { join } from "node:path";
import { testStyles } from "../../testing";

testStyles(join(import.meta.dir, ".."), {
  prefix: "labui",
  input: "web/styles.css",
  global: /^(\.?rtc-|\.wterm|\.term-|cursor-(line-)?blink$|:is\(\.wterm)/,
});

import { expect, it } from "bun:test";
import { renderToStaticMarkup } from "react-dom/server";
import { Back, Table, column } from "@rtcbench/admin";

it("keeps caller URLs intact and renders tables without app dependencies", () => {
  const html = renderToStaticMarkup(
    <>
      <Back href="/projects" label="Projects" />
      <Table
        label="Projects"
        rows={[{ name: "One" }, { name: "Two" }]}
        rowKey={(row) => row.name}
        columns={[column("name", "Name", (row: { name: string }) => row.name)]}
      />
    </>,
  );
  expect(html).toContain('href="/projects"');
  expect(html).toContain('aria-label="Projects"');
  expect(html).toContain("Two");
});

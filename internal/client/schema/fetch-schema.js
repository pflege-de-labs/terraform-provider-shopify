// Prints the Admin API schema as SDL for genqlient; run only when bumping the pinned version.
// Usage (in a scratch dir): npm i graphql@16 && node fetch-schema.js 2026-10 > admin-2026-10.graphql
const g = require("graphql");

(async () => {
  const url = `https://shopify.dev/admin-graphql-direct-proxy/${process.argv[2]}`;
  const res = await fetch(url, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ query: g.getIntrospectionQuery() }),
  });
  const j = await res.json();
  if (!j.data) {
    console.error(JSON.stringify(j).slice(0, 500));
    process.exit(1);
  }
  process.stdout.write(g.printSchema(g.buildClientSchema(j.data)) + "\n");
})();

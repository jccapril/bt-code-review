// SPDX-License-Identifier: Apache-2.0
const {spawnSync} = require("node:child_process");
const {mkdirSync} = require("node:fs");
const path = require("node:path");
const root = path.resolve(__dirname,"..");
mkdirSync(path.join(root,"dist"), {recursive:true});
for (const [platform,goos] of [["darwin","darwin"],["linux","linux"],["win32","windows"]]) {
  for (const [arch,goarch] of [["arm64","arm64"],["x64","amd64"]]) {
    const output = path.join("dist","btcr-"+platform+"-"+arch+(platform==="win32"?".exe":""));
    const result = spawnSync("go", ["build","-trimpath","-ldflags=-s -w","-o",output,"."], {
      cwd:root,stdio:"inherit",env:{...process.env,CGO_ENABLED:"0",GOOS:goos,GOARCH:goarch}
    });
    if (result.error) throw result.error;
    if (result.status!==0) process.exit(result.status ?? 1);
  }
}

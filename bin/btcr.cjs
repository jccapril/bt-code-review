#!/usr/bin/env node
// SPDX-License-Identifier: Apache-2.0
const {spawn} = require("node:child_process");
const path = require("node:path");
const binary = path.join(__dirname,"..","dist","btcr-"+process.platform+"-"+process.arch+(process.platform==="win32"?".exe":""));
const child = spawn(binary,process.argv.slice(2),{stdio:"inherit"});
for (const signal of ["SIGINT","SIGTERM"]) process.on(signal,()=>child.kill(signal));
child.on("error",e=>{console.error("btcr:",e.message);process.exitCode=1});
child.on("exit",(code,signal)=>{process.exitCode=code ?? (signal==="SIGINT"?130:143)});

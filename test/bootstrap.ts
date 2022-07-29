const [testDir] = Deno.args;

startEsmServer(async (p) => {
  console.log("esm.sh server started");
  try {
    if (testDir) {
      await runTest(testDir, p, true);
    } else {
      for await (const entry of Deno.readDir("./test/deno")) {
        if (entry.isDirectory) {
          await runTest(entry.name, p);
        }
      }
    }
    console.log("Done!");
  } catch (error) {
    console.error(error);
  }
  p.kill("SIGINT");
});

async function startEsmServer(onReady: (p: any) => void) {
  await run("go", "build", "-o", "esmd", "main.go");
  const p = Deno.run({
    cmd: ["./esmd", "--port", "8080"],
    stdout: "null",
    stderr: "inherit",
  });
  while (true) {
    try {
      await new Promise((resolve) => setTimeout(resolve, 500));
      const { ns } = await fetch(`http://localhost:8080/status.json`).then((
        res,
      ) => res.json());
      if (ns?.ready) {
        onReady(p);
        break;
      }
    } catch (_) {}
  }
  await p.status();
}

async function runTest(name: string, p: any, retry?: boolean) {
  const cmd = [
    Deno.execPath(),
    "test",
    "-A",
    "--check=all",
    "--unstable",
    "--reload=http://localhost:8080",
    "--location=http://0.0.0.0/",
  ];
  const dir = `test/deno/${name}/`;
  if (await existsFile(dir + "deno.json")) {
    cmd.push("--config", dir + "deno.json");
  }
  cmd.push(dir);

  console.log(`\n[testing ${name}]`);

  const { code, success } = await run(...cmd);
  if (!success) {
    if (!retry) {
      console.log("something wrong, retry...");
      await new Promise((resolve) => setTimeout(resolve, 100));
      await runTest(name, p, true);
    } else {
      p.kill("SIGINT");
      Deno.exit(code);
    }
  }
}

async function run(...cmd: string[]) {
  return await Deno.run({ cmd, stdout: "inherit", stderr: "inherit" }).status();
}

/* check whether or not the given path exists as regular file. */
export async function existsFile(path: string): Promise<boolean> {
  try {
    const fi = await Deno.lstat(path);
    return fi.isFile;
  } catch (err) {
    if (err instanceof Deno.errors.NotFound) {
      return false;
    }
    throw err;
  }
}
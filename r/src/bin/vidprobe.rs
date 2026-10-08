use std::process::exit;

fn main() {
    let args: Vec<String> = std::env::args().skip(1).collect();
    if args.is_empty() {
        eprintln!("usage: vidprobe FILE...");
        exit(2);
    }
    let mut status = 0;
    for p in &args {
        match pygorvid::probe_file(p) {
            Ok(f) => println!("{p}: {f}"),
            Err(e) => {
                eprintln!("{p}: {e}");
                status = 1;
            }
        }
    }
    exit(status);
}

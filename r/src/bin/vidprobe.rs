use std::process::exit;

fn main() {
    let args: Vec<String> = std::env::args().skip(1).collect();
    if args.is_empty() {
        eprintln!("usage: vidprobe FILE...");
        exit(2);
    }
    let mut status = 0;
    for p in &args {
        let mut v = pygorvid::open_file(p);
        if v.isopen() {
            let info = v.getbasicinfo();
            let err = v.errorinfo().0;
            if err.is_empty() {
                println!("{p}: {} {info:?}", v.format());
            } else {
                eprintln!("{p}: {}: {err}", v.format());
                status = 1;
            }
        } else {
            eprintln!("{p}: {}", v.errorinfo().0);
            status = 1;
        }
    }
    exit(status);
}

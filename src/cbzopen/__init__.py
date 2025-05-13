import argparse
import http.server
import os
import pathlib
import socketserver
import sys
import tempfile
import time
import webbrowser
import zipfile


def _create_index_html(directory: pathlib.Path) -> None:
    """Create an index.html file with all images in the directory."""
    image_files = sorted(
        path.relative_to(directory)
        for path in pathlib.Path(directory).iterdir()
        if path.is_file()
        and path.suffix in {".jpg", ".jpeg", ".png", ".gif", ".webp", ".avif"}
    )
    template = pathlib.Path(__file__).joinpath("..", "index.html").resolve().read_text()
    images = "\n".join(
        f'<img src="{img_file}" alt="{img_file}"><br>' for img_file in image_files
    )
    content = template.replace("{{ images }}", images)
    directory.joinpath("index.html").write_text(content)


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument(
        "file",
        type=pathlib.Path,
        help="cbz file",
    )
    parser.add_argument("--open", action="store_true", help="open web browser")
    parser.add_argument(
        "-p",
        "--port",
        type=int,
        default=0,
        help="port to serve on (default: random port)",
    )
    args = parser.parse_args()

    # Check if the file exists
    if not args.file.exists():
        print(f"Error: File '{args.file}' does not exist", file=sys.stderr)
        sys.exit(1)

    # Check if the file is a valid zip file
    if not zipfile.is_zipfile(args.file):
        print(f"Error: File '{args.file}' is not a valid zip file", file=sys.stderr)
        sys.exit(1)

    # Create a temporary directory
    with tempfile.TemporaryDirectory(ignore_cleanup_errors=True) as temp_dir:
        print(f"Extracting {args.file} to {temp_dir}")

        # Extract the zip file
        with zipfile.ZipFile(args.file, "r") as zip_ref:
            zip_ref.extractall(temp_dir)

        # Create index.html with all extracted images
        _create_index_html(pathlib.Path(temp_dir))

        # Change to the temporary directory
        os.chdir(temp_dir)

        # Start an HTTP server
        handler = http.server.SimpleHTTPRequestHandler
        with socketserver.TCPServer(("", args.port), handler) as httpd:
            _, port = httpd.server_address
            server_url = f"http://localhost:{port}/index.html"
            print(f"Server running on {server_url}")

            if args.open:
                print("Opening web browser...")
                webbrowser.open(server_url)

            print("Press Ctrl+C to stop server")

            try:
                httpd.serve_forever()
            except KeyboardInterrupt:
                print("\nShutting down server...")
                time.sleep(2)
                httpd.shutdown()
                httpd.server_close()
                print("Server stopped.")

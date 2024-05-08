load("@bazel_gazelle//:deps.bzl", "go_repository")

def go_dependencies():
    go_repository(
        name = "com_github_ajstarks_svgo",
        importpath = "github.com/ajstarks/svgo",
        sum = "h1:slYM766cy2nI3BwyRiyQj/Ud48djTMtMebDqepE95rw=",
        version = "v0.0.0-20211024235047-1546f124cd8b",
    )
    go_repository(
        name = "com_github_go_fonts_liberation",
        importpath = "github.com/go-fonts/liberation",
        sum = "h1:3BI2iaE7R/s6uUUtzNCjo3QijJu3aS4wmrMgfSpYQ+8=",
        version = "v0.3.0",
    )
    go_repository(
        name = "com_github_go_latex_latex",
        importpath = "github.com/go-latex/latex",
        sum = "h1:NxXI5pTAtpEaU49bpLpQoDsu1zrteW/vxzTz8Cd2UAs=",
        version = "v0.0.0-20230307184459-12ec69307ad9",
    )
    go_repository(
        name = "com_github_go_pdf_fpdf",
        importpath = "github.com/go-pdf/fpdf",
        sum = "h1:MlgtGIfsdMEEQJr2le6b/HNr1ZlQwxyWr77r2aj2U/8=",
        version = "v0.6.0",
    )
    go_repository(
        name = "com_github_goccmack_gocc",
        importpath = "github.com/goccmack/gocc",
        sum = "h1:FSii2UQeSLngl3jFoR4tUKZLprO7qUlh/TKKticc0BM=",
        version = "v0.0.0-20230228185258-2292f9e40198",
    )
    go_repository(
        name = "com_github_golang_freetype",
        importpath = "github.com/golang/freetype",
        sum = "h1:DACJavvAHhabrF08vX0COfcOBJRhZ8lUbR+ZWIs0Y5g=",
        version = "v0.0.0-20170609003504-e2365dfdc4a0",
    )
    go_repository(
        name = "com_github_google_go_cmp",
        importpath = "github.com/google/go-cmp",
        sum = "h1:e6P7q2lk1O+qJJb4BtCQXlK8vWEO8V1ZeuEdJNOqZyg=",
        version = "v0.5.8",
    )
    go_repository(
        name = "ht_sr_git_sbinet_gg",
        importpath = "git.sr.ht/~sbinet/gg",
        sum = "h1:LNhjNn8DerC8f9DHLz6lS0YYul/b602DUxDgGkd/Aik=",
        version = "v0.3.1",
    )
    go_repository(
        name = "io_rsc_pdf",
        importpath = "rsc.io/pdf",
        sum = "h1:k1MczvYDUvJBe93bYd7wrZLLUEcLZAuF824/I4e5Xr4=",
        version = "v0.1.1",
    )
    go_repository(
        name = "org_golang_x_exp",
        importpath = "golang.org/x/exp",
        sum = "h1:GoHiUyI/Tp2nVkLI2mCxVkOjsbSXD66ic0XW0js0R9g=",
        version = "v0.0.0-20230905200255-921286631fa9",
    )
    go_repository(
        name = "org_golang_x_image",
        importpath = "golang.org/x/image",
        sum = "h1:bR8b5okrPI3g/gyZakLZHeWxAR8Dn5CyxXv1hLH5g/4=",
        version = "v0.6.0",
    )
    go_repository(
        name = "org_golang_x_mod",
        importpath = "golang.org/x/mod",
        sum = "h1:rmsUpXtvNzj340zd98LZ4KntptpfRHwpFOHG188oHXc=",
        version = "v0.12.0",
    )
    go_repository(
        name = "org_golang_x_sys",
        importpath = "golang.org/x/sys",
        sum = "h1:CM0HF96J0hcLAwsHPJZjfdNzs0gftsLfgKt57wWHJ0o=",
        version = "v0.12.0",
    )
    go_repository(
        name = "org_golang_x_text",
        importpath = "golang.org/x/text",
        sum = "h1:57P1ETyNKtuIjB4SRd15iJxuhj8Gc416Y78H3qgMh68=",
        version = "v0.8.0",
    )
    go_repository(
        name = "org_golang_x_tools",
        importpath = "golang.org/x/tools",
        sum = "h1:Iey4qkscZuv0VvIt8E0neZjtPVQFSc870HQ448QgEmQ=",
        version = "v0.13.0",
    )
    go_repository(
        name = "org_gonum_v1_gonum",
        importpath = "gonum.org/v1/gonum",
        sum = "h1:2NiG67LD1tEH0D7kM+ps2V+fXmsAnpUeec7n8tcr4S0=",
        version = "v0.14.0",
    )
    go_repository(
        name = "org_gonum_v1_plot",
        importpath = "gonum.org/v1/plot",
        sum = "h1:dnifSs43YJuNMDzB7v8wV64O4ABBHReuAVAoBxqBqS4=",
        version = "v0.10.1",
    )

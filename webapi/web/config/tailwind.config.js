/** @type {import('tailwindcss').Config} */
module.exports = {
  darkMode: "class",
  content: ["../**/*.html", "../js/**/*.js", "../components/**/*.html"],
  theme: {
    extend: {
      colors: {
        "primary": "#f9f506",
        "background-light": "#f8f8f5",
        "background-dark": "#23220f",
        "card-light": "#ffffff",
        "card-dark": "#2d2c16",
        "text-main-light": "#1c1c0d",
        "text-main-dark": "#fcfcf8",
        "text-sub-light": "#6d6d4e",
        "text-sub-dark": "#bfbfad",
        "border-light": "#e9e8ce",
        "border-dark": "#3e3d24",
      },
      fontFamily: {
        "display": ["Spline Sans", "sans-serif"],
        "body": ["Noto Sans", "sans-serif"],
      },
      borderRadius: { "DEFAULT": "1rem", "lg": "2rem", "xl": "3rem", "full": "9999px" },
    },
  },
  plugins: [],
}